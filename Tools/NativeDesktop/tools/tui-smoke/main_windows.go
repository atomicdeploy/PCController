//go:build windows

// tui-smoke drives the shipped PCControllerDesktop terminal UI through a real Windows
// pseudoconsole. It intentionally lives outside internal/tui: the smoke test
// observes the same executable, terminal detection, VT stream, resize events,
// and keyboard byte path that an interactive terminal uses.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	defaultColumns       = 100
	defaultRows          = 30
	maxTranscriptBytes   = 16 << 20
	forcedTermination    = 124
	waitTimeoutResult    = 258
	processPollInterval  = 25 * time.Millisecond
	outputSettleInterval = 80 * time.Millisecond
	conPTYChildMode      = "--conpty-child"
)

type options struct {
	executable   string
	configPath   string
	transcript   string
	totalTimeout time.Duration
	stepTimeout  time.Duration
	keepConfig   bool
	verbose      bool
}

type smokeRunner struct {
	process  *pseudoConsoleProcess
	options  options
	started  time.Time
	deadline time.Time
	steps    []string
}

type transcriptBuffer struct {
	mu        sync.Mutex
	data      []byte
	truncated bool
}

func (b *transcriptBuffer) append(data []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(data) >= maxTranscriptBytes {
		b.data = append(b.data[:0], data[len(data)-maxTranscriptBytes:]...)
		b.truncated = true
		return
	}
	if excess := len(b.data) + len(data) - maxTranscriptBytes; excess > 0 {
		copy(b.data, b.data[excess:])
		b.data = b.data[:len(b.data)-excess]
		b.truncated = true
	}
	b.data = append(b.data, data...)
}

func (b *transcriptBuffer) snapshot() ([]byte, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	result := append([]byte(nil), b.data...)
	return result, b.truncated
}

func (b *transcriptBuffer) length() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.data)
}

type pseudoConsoleProcess struct {
	pseudoConsole windows.Handle
	process       windows.Handle
	job           windows.Handle
	input         *os.File
	output        *os.File
	transcript    transcriptBuffer
	readDone      chan error
	closeOnce     sync.Once
}

func main() {
	if len(os.Args) >= 3 && os.Args[1] == conPTYChildMode {
		os.Exit(runConPTYChild(os.Args[2:]))
	}
	opts := parseOptions()
	if err := run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "❌ TUI smoke test failed: %v\n", err)
		os.Exit(1)
	}
}

func parseOptions() options {
	var opts options
	flag.StringVar(
		&opts.executable,
		"exe",
		filepath.Join("dist", "PCControllerDesktop.exe"),
		"PCControllerDesktop executable to launch",
	)
	flag.StringVar(
		&opts.configPath,
		"config",
		"",
		"isolated configuration path (a temporary path is used by default)",
	)
	flag.StringVar(
		&opts.transcript,
		"transcript",
		"",
		"optional path for the raw VT transcript",
	)
	flag.DurationVar(
		&opts.totalTimeout,
		"timeout",
		45*time.Second,
		"maximum duration for the complete smoke run",
	)
	flag.DurationVar(
		&opts.stepTimeout,
		"step-timeout",
		7*time.Second,
		"maximum duration for one screen or input assertion",
	)
	flag.BoolVar(
		&opts.keepConfig,
		"keep-config",
		false,
		"retain the temporary isolated configuration directory",
	)
	flag.BoolVar(&opts.verbose, "verbose", false, "print every successful step")
	flag.Parse()
	return opts
}

func run(opts options) (runErr error) {
	if opts.totalTimeout <= 0 {
		return errors.New("-timeout must be greater than zero")
	}
	if opts.stepTimeout <= 0 {
		return errors.New("-step-timeout must be greater than zero")
	}

	executable, err := filepath.Abs(opts.executable)
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	info, err := os.Stat(executable)
	if err != nil {
		return fmt.Errorf("inspect executable %q: %w", executable, err)
	}
	if info.IsDir() {
		return fmt.Errorf("executable %q is a directory", executable)
	}

	configPath, cleanupConfig, err := isolatedConfigPath(opts)
	if err != nil {
		return err
	}
	defer cleanupConfig()

	workingDirectory := filepath.Dir(executable)
	args := []string{
		executable,
		"tui",
		"dashboard",
		"--mode", "standalone",
		"--config", configPath,
		"--theme", "dark",
		"--no-mouse",
	}
	launcher, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve smoke launcher: %w", err)
	}
	// PCControllerDesktop deliberately audits its console ownership before entering TUI
	// mode. Keeping this tiny launcher attached to ConPTY while PCControllerDesktop runs
	// gives that audit the same parent/child topology as cmd.exe or Windows
	// Terminal; launching PCControllerDesktop as ConPTY's sole client would make its
	// legacy detached-launch fallback allocate or attach a different console.
	conPTYArgs := append([]string{launcher, conPTYChildMode}, args...)
	process, err := startPseudoConsole(
		conPTYArgs,
		workingDirectory,
		windows.Coord{X: defaultColumns, Y: defaultRows},
	)
	if err != nil {
		return fmt.Errorf("launch ConPTY process: %w", err)
	}
	defer func() {
		if runErr != nil {
			process.terminate()
		}
		process.close()
	}()

	runner := &smokeRunner{
		process:  process,
		options:  opts,
		started:  time.Now(),
		deadline: time.Now().Add(opts.totalTimeout),
	}
	defer func() {
		if err := writeTranscript(opts.transcript, process); err != nil {
			runErr = errors.Join(runErr, err)
		}
	}()

	if err := runner.exercise(); err != nil {
		return err
	}
	exitCode, err := process.wait(runner.remaining())
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("PCControllerDesktop exited with code %d", exitCode)
	}
	if err := runner.verifyCleanup(); err != nil {
		return err
	}

	fmt.Printf(
		"✅ PCControllerDesktop TUI passed %d ConPTY checks in %s\n",
		len(runner.steps),
		time.Since(runner.started).Round(time.Millisecond),
	)
	fmt.Printf("   executable: %s\n", executable)
	fmt.Printf("   config:     %s\n", configPath)
	fmt.Printf("   screens:    dashboard, commands, settings, device, serving, diagnostics, terminal\n")
	fmt.Printf("   resizes:    80x24, 40x12, 23x7, 120x40\n")
	return nil
}

func runConPTYChild(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "tui-smoke child: missing executable")
		return 127
	}
	command := exec.Command(args[0], args[1:]...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return exitError.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "tui-smoke child: %v\n", err)
		return 127
	}
	return 0
}

func isolatedConfigPath(opts options) (string, func(), error) {
	if strings.TrimSpace(opts.configPath) != "" {
		path, err := filepath.Abs(opts.configPath)
		if err != nil {
			return "", func() {}, fmt.Errorf("resolve config path: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", func() {}, fmt.Errorf("create config directory: %w", err)
		}
		return path, func() {}, nil
	}

	directory, err := os.MkdirTemp("", "pccontroller-desktop-tui-smoke-")
	if err != nil {
		return "", func() {}, fmt.Errorf("create isolated config directory: %w", err)
	}
	cleanup := func() {
		if opts.keepConfig {
			fmt.Printf("   retained:   %s\n", directory)
			return
		}
		removeTemporaryConfigDirectory(directory)
	}
	return filepath.Join(directory, "config.json"), cleanup, nil
}

func removeTemporaryConfigDirectory(directory string) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return
	}
	temporaryRoot, err := filepath.Abs(os.TempDir())
	if err != nil {
		return
	}
	relative, err := filepath.Rel(temporaryRoot, absolute)
	if err != nil ||
		relative == "." ||
		relative == "" ||
		relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) ||
		!strings.HasPrefix(filepath.Base(absolute), "pccontroller-desktop-tui-smoke-") {
		return
	}
	_ = os.RemoveAll(absolute)
}

func writeTranscript(path string, process *pseudoConsoleProcess) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve transcript path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		return fmt.Errorf("create transcript directory: %w", err)
	}
	data, _ := process.transcript.snapshot()
	if err := os.WriteFile(absolute, data, 0o644); err != nil {
		return fmt.Errorf("write VT transcript: %w", err)
	}
	return nil
}

func (r *smokeRunner) exercise() error {
	if err := r.expect("dashboard", []string{"STATUS & CONTROL"}, 0); err != nil {
		return err
	}

	if err := r.openScreen(
		"commands",
		":",
		[]string{"COMMAND CENTER"},
		true,
		false,
	); err != nil {
		return err
	}
	if err := r.openScreen(
		"settings",
		"s",
		[]string{"APPLICATION SETTINGS", "Choose a configuration workspace"},
		true,
		false,
	); err != nil {
		return err
	}
	if err := r.openScreen(
		"device",
		"v",
		[]string{"DEVICE SETTINGS"},
		true,
		false,
	); err != nil {
		return err
	}
	if err := r.openScreen(
		"serving",
		"w",
		[]string{"SERVING & INTEGRATION"},
		false,
		true,
	); err != nil {
		return err
	}
	if err := r.openScreen(
		"diagnostics",
		"d",
		[]string{"DIAGNOSTICS", "FETCH"},
		false,
		false,
	); err != nil {
		return err
	}
	if err := r.openScreen(
		"terminal",
		"x",
		[]string{"DEVICE TERMINAL"},
		false,
		false,
	); err != nil {
		return err
	}

	if err := r.resize(80, 24, []string{"STATUS & CONTROL"}); err != nil {
		return err
	}
	if err := r.resize(40, 12, []string{"PCController"}); err != nil {
		return err
	}
	if err := r.resize(23, 7, []string{"PCCONTROLLER_DESKTOP"}); err != nil {
		return err
	}
	if err := r.resize(120, 40, []string{"STATUS & CONTROL"}); err != nil {
		return err
	}

	if err := r.send("quit", "q"); err != nil {
		return err
	}
	return nil
}

func (r *smokeRunner) openScreen(
	name string,
	key string,
	identity []string,
	testNavigation bool,
	closeViaSettings bool,
) error {
	mark := r.process.transcript.length()
	if err := r.send("open "+name, key); err != nil {
		return err
	}
	if err := r.expect(name, identity, mark); err != nil {
		return err
	}

	if testNavigation {
		if err := r.requireRepaint(name+" down", "\x1b[B"); err != nil {
			return err
		}
		if err := r.requireRepaint(name+" up", "\x1b[A"); err != nil {
			return err
		}
	}

	if closeViaSettings {
		mark := r.process.transcript.length()
		if err := r.send("close "+name+" editor", "\x1b"); err != nil {
			return err
		}
		if err := r.expect(
			name+" categories",
			[]string{"APPLICATION SETTINGS", "Choose a configuration workspace"},
			mark,
		); err != nil {
			return err
		}
	}

	mark = r.process.transcript.length()
	if err := r.send("close "+name, "\x1b"); err != nil {
		return err
	}
	return r.expect(name+" close", []string{"STATUS & CONTROL"}, mark)
}

func (r *smokeRunner) resize(columns, rows int16, identity []string) error {
	mark := r.process.transcript.length()
	if err := windows.ResizePseudoConsole(
		r.process.pseudoConsole,
		windows.Coord{X: columns, Y: rows},
	); err != nil {
		return fmt.Errorf("resize ConPTY to %dx%d: %w", columns, rows, err)
	}
	label := fmt.Sprintf("resize %dx%d", columns, rows)
	return r.expect(label, identity, mark)
}

func (r *smokeRunner) requireRepaint(label, key string) error {
	mark := r.process.transcript.length()
	if err := r.send(label, key); err != nil {
		return err
	}
	deadline := time.Now().Add(r.stepDuration())
	for time.Now().Before(deadline) {
		if r.process.transcript.length() > mark {
			time.Sleep(outputSettleInterval)
			r.record("repaint " + label)
			return nil
		}
		if exited, code, err := r.process.exitStatus(); err != nil {
			return err
		} else if exited {
			return fmt.Errorf("%s: PCControllerDesktop exited early with code %d", label, code)
		}
		time.Sleep(processPollInterval)
	}
	return fmt.Errorf("%s: key produced no terminal repaint", label)
}

func (r *smokeRunner) send(label, input string) error {
	if r.remaining() <= 0 {
		return fmt.Errorf("%s: total smoke timeout expired", label)
	}
	if _, err := r.process.input.Write([]byte(input)); err != nil {
		return fmt.Errorf("%s: write key bytes: %w", label, err)
	}
	r.record(label)
	return nil
}

func (r *smokeRunner) expect(label string, needles []string, mark int) error {
	deadline := time.Now().Add(r.stepDuration())
	for time.Now().Before(deadline) {
		raw, truncated := r.process.transcript.snapshot()
		if mark > len(raw) {
			if truncated {
				mark = 0
			} else {
				mark = len(raw)
			}
		}
		plain := strings.ToUpper(stripVT(raw[mark:]))
		found := true
		for _, needle := range needles {
			if !strings.Contains(plain, strings.ToUpper(needle)) {
				found = false
				break
			}
		}
		if found {
			r.record("render " + label)
			return nil
		}

		if exited, code, err := r.process.exitStatus(); err != nil {
			return err
		} else if exited {
			return fmt.Errorf(
				"%s: PCControllerDesktop exited early with code %d; output tail: %q",
				label,
				code,
				plainTail(plain, 600),
			)
		}
		time.Sleep(processPollInterval)
	}

	raw, _ := r.process.transcript.snapshot()
	if mark > len(raw) {
		mark = 0
	}
	return fmt.Errorf(
		"%s: did not render %q within %s; output tail: %q",
		label,
		strings.Join(needles, " + "),
		r.stepDuration().Round(time.Millisecond),
		plainTail(stripVT(raw[mark:]), 900),
	)
}

func (r *smokeRunner) verifyCleanup() error {
	raw, truncated := r.process.transcript.snapshot()
	if truncated {
		return errors.New("VT transcript exceeded its bounded capture; cleanup cannot be verified")
	}

	altEnter := []byte("\x1b[?1049h")
	altExit := []byte("\x1b[?1049l")
	cursorHide := []byte("\x1b[?25l")
	cursorShow := []byte("\x1b[?25h")

	enters := bytes.Count(raw, altEnter)
	exits := bytes.Count(raw, altExit)
	if enters == 0 {
		return errors.New("VT cleanup: alternate-screen entry was not observed")
	}
	if exits < enters || bytes.LastIndex(raw, altExit) < bytes.LastIndex(raw, altEnter) {
		return fmt.Errorf(
			"VT cleanup: alternate screen is unbalanced (enter=%d exit=%d)",
			enters,
			exits,
		)
	}

	hides := bytes.Count(raw, cursorHide)
	shows := bytes.Count(raw, cursorShow)
	if hides > 0 &&
		(shows < hides || bytes.LastIndex(raw, cursorShow) < bytes.LastIndex(raw, cursorHide)) {
		return fmt.Errorf(
			"VT cleanup: cursor visibility is unbalanced (hide=%d show=%d)",
			hides,
			shows,
		)
	}
	r.record("balanced VT cleanup")
	return nil
}

func (r *smokeRunner) stepDuration() time.Duration {
	remaining := r.remaining()
	if remaining < r.options.stepTimeout {
		return remaining
	}
	return r.options.stepTimeout
}

func (r *smokeRunner) remaining() time.Duration {
	remaining := time.Until(r.deadline)
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (r *smokeRunner) record(step string) {
	r.steps = append(r.steps, step)
	if r.options.verbose {
		fmt.Printf("✓ %s\n", step)
	}
}

func startPseudoConsole(
	args []string,
	workingDirectory string,
	size windows.Coord,
) (_ *pseudoConsoleProcess, resultErr error) {
	if len(args) == 0 {
		return nil, errors.New("empty command line")
	}

	var inputRead windows.Handle
	var inputWrite windows.Handle
	var outputRead windows.Handle
	var outputWrite windows.Handle
	var pseudoConsole windows.Handle
	var job windows.Handle
	closeHandle := func(handle *windows.Handle) {
		if *handle != 0 && *handle != windows.InvalidHandle {
			_ = windows.CloseHandle(*handle)
			*handle = 0
		}
	}
	defer func() {
		closeHandle(&inputRead)
		closeHandle(&inputWrite)
		closeHandle(&outputRead)
		closeHandle(&outputWrite)
		if resultErr != nil && pseudoConsole != 0 {
			windows.ClosePseudoConsole(pseudoConsole)
		}
		if resultErr != nil {
			closeHandle(&job)
		}
	}()

	if err := windows.CreatePipe(&inputRead, &inputWrite, nil, 0); err != nil {
		return nil, fmt.Errorf("create ConPTY input pipe: %w", err)
	}
	if err := windows.CreatePipe(&outputRead, &outputWrite, nil, 0); err != nil {
		return nil, fmt.Errorf("create ConPTY output pipe: %w", err)
	}
	if err := windows.CreatePseudoConsole(
		size,
		inputRead,
		outputWrite,
		0,
		&pseudoConsole,
	); err != nil {
		return nil, fmt.Errorf("CreatePseudoConsole: %w", err)
	}
	closeHandle(&inputRead)
	closeHandle(&outputWrite)

	attributes, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return nil, fmt.Errorf("allocate process attribute list: %w", err)
	}
	defer attributes.Delete()
	// HPCON is a pointer-shaped kernel handle and Win32 requires its value
	// itself (not &pseudoConsole) as the attribute payload. Reinterpret the
	// handle storage as a pointer-shaped value so we preserve those exact bits
	// without a uintptr-to-pointer conversion that is invalid for ordinary Go
	// memory and correctly rejected by go vet's unsafeptr analyzer.
	pseudoConsoleValue := *(*unsafe.Pointer)(unsafe.Pointer(&pseudoConsole))
	if err := attributes.Update(
		windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE,
		pseudoConsoleValue,
		unsafe.Sizeof(pseudoConsole),
	); err != nil {
		return nil, fmt.Errorf("attach pseudoconsole process attribute: %w", err)
	}

	commandLine, err := windows.UTF16PtrFromString(
		windows.ComposeCommandLine(args),
	)
	if err != nil {
		return nil, fmt.Errorf("encode command line: %w", err)
	}
	application, err := windows.UTF16PtrFromString(args[0])
	if err != nil {
		return nil, fmt.Errorf("encode executable path: %w", err)
	}
	currentDirectory, err := windows.UTF16PtrFromString(workingDirectory)
	if err != nil {
		return nil, fmt.Errorf("encode working directory: %w", err)
	}

	startup := windows.StartupInfoEx{
		StartupInfo: windows.StartupInfo{
			Cb:    uint32(unsafe.Sizeof(windows.StartupInfoEx{})),
			Flags: windows.STARTF_USESTDHANDLES,
		},
		ProcThreadAttributeList: attributes.List(),
	}
	var processInfo windows.ProcessInformation
	job, err = createTerminationJob()
	if err != nil {
		return nil, err
	}
	if err := windows.CreateProcess(
		application,
		commandLine,
		nil,
		nil,
		false,
		windows.EXTENDED_STARTUPINFO_PRESENT|
			windows.CREATE_UNICODE_ENVIRONMENT|
			windows.CREATE_NEW_PROCESS_GROUP|
			windows.CREATE_SUSPENDED,
		nil,
		currentDirectory,
		&startup.StartupInfo,
		&processInfo,
	); err != nil {
		return nil, fmt.Errorf("CreateProcessW: %w", err)
	}
	if err := windows.AssignProcessToJobObject(job, processInfo.Process); err != nil {
		_ = windows.TerminateProcess(processInfo.Process, forcedTermination)
		_ = windows.CloseHandle(processInfo.Thread)
		_ = windows.CloseHandle(processInfo.Process)
		return nil, fmt.Errorf("assign ConPTY process tree to cleanup job: %w", err)
	}
	if _, err := windows.ResumeThread(processInfo.Thread); err != nil {
		_ = windows.TerminateJobObject(job, forcedTermination)
		_ = windows.CloseHandle(processInfo.Thread)
		_ = windows.CloseHandle(processInfo.Process)
		return nil, fmt.Errorf("resume ConPTY process: %w", err)
	}
	_ = windows.CloseHandle(processInfo.Thread)

	input := os.NewFile(uintptr(inputWrite), "pccontroller-desktop-conpty-input")
	output := os.NewFile(uintptr(outputRead), "pccontroller-desktop-conpty-output")
	if input == nil || output == nil {
		_ = windows.TerminateProcess(processInfo.Process, forcedTermination)
		_ = windows.CloseHandle(processInfo.Process)
		if input != nil {
			_ = input.Close()
			inputWrite = 0
		}
		if output != nil {
			_ = output.Close()
			outputRead = 0
		}
		return nil, errors.New("wrap ConPTY pipe handles")
	}
	inputWrite = 0
	outputRead = 0

	process := &pseudoConsoleProcess{
		pseudoConsole: pseudoConsole,
		process:       processInfo.Process,
		job:           job,
		input:         input,
		output:        output,
		readDone:      make(chan error, 1),
	}
	pseudoConsole = 0
	job = 0
	go process.readOutput()
	return process, nil
}

func createTerminationJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, fmt.Errorf("create ConPTY cleanup job: %w", err)
	}
	information := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	information.BasicLimitInformation.LimitFlags =
		windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	result, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&information)),
		uint32(unsafe.Sizeof(information)),
	)
	if err != nil || result == 0 {
		_ = windows.CloseHandle(job)
		if err == nil {
			err = errors.New("SetInformationJobObject returned false")
		}
		return 0, fmt.Errorf("configure ConPTY cleanup job: %w", err)
	}
	return job, nil
}

func (p *pseudoConsoleProcess) readOutput() {
	buffer := make([]byte, 32<<10)
	for {
		count, err := p.output.Read(buffer)
		if count > 0 {
			p.transcript.append(buffer[:count])
		}
		if err != nil {
			p.readDone <- err
			return
		}
	}
}

func (p *pseudoConsoleProcess) exitStatus() (bool, uint32, error) {
	event, err := windows.WaitForSingleObject(p.process, 0)
	if err != nil {
		return false, 0, fmt.Errorf("poll PCControllerDesktop process: %w", err)
	}
	if event != windows.WAIT_OBJECT_0 {
		return false, 0, nil
	}
	var exitCode uint32
	if err := windows.GetExitCodeProcess(p.process, &exitCode); err != nil {
		return false, 0, fmt.Errorf("read PCControllerDesktop exit code: %w", err)
	}
	return true, exitCode, nil
}

func (p *pseudoConsoleProcess) wait(timeout time.Duration) (uint32, error) {
	if timeout <= 0 {
		p.terminate()
		return 0, errors.New("PCControllerDesktop exit exceeded the total smoke timeout")
	}
	milliseconds := timeout.Milliseconds()
	if milliseconds > int64(^uint32(0)-1) {
		milliseconds = int64(^uint32(0) - 1)
	}
	event, err := windows.WaitForSingleObject(p.process, uint32(milliseconds))
	if err != nil {
		return 0, fmt.Errorf("wait for PCControllerDesktop: %w", err)
	}
	if event == waitTimeoutResult {
		p.terminate()
		return 0, fmt.Errorf("PCControllerDesktop did not exit within %s after q", timeout.Round(time.Millisecond))
	}
	if event != windows.WAIT_OBJECT_0 {
		return 0, fmt.Errorf("wait for PCControllerDesktop returned unexpected status %#x", event)
	}
	var exitCode uint32
	if err := windows.GetExitCodeProcess(p.process, &exitCode); err != nil {
		return 0, fmt.Errorf("read PCControllerDesktop exit code: %w", err)
	}
	return exitCode, nil
}

func (p *pseudoConsoleProcess) terminate() {
	exited, _, err := p.exitStatus()
	if err == nil && !exited {
		_ = windows.TerminateJobObject(p.job, forcedTermination)
		_, _ = windows.WaitForSingleObject(p.process, 2_000)
	}
}

func (p *pseudoConsoleProcess) close() {
	p.closeOnce.Do(func() {
		_ = p.input.Close()
		windows.ClosePseudoConsole(p.pseudoConsole)
		_ = p.output.Close()
		_ = windows.CloseHandle(p.process)
		_ = windows.CloseHandle(p.job)
	})
}

func stripVT(raw []byte) string {
	result := make([]byte, 0, len(raw))
	for index := 0; index < len(raw); {
		current := raw[index]
		if current != 0x1b {
			if current >= 0x20 || current == '\n' || current == '\r' || current == '\t' {
				result = append(result, current)
			}
			index++
			continue
		}
		index++
		if index >= len(raw) {
			break
		}
		switch raw[index] {
		case '[':
			index++
			for index < len(raw) {
				final := raw[index]
				index++
				if final >= 0x40 && final <= 0x7e {
					break
				}
			}
		case ']':
			index = skipStringControl(raw, index+1)
		case 'P', 'X', '^', '_':
			index = skipStringControl(raw, index+1)
		default:
			index++
		}
	}
	return string(result)
}

func skipStringControl(raw []byte, index int) int {
	for index < len(raw) {
		if raw[index] == 0x07 {
			return index + 1
		}
		if raw[index] == 0x1b && index+1 < len(raw) && raw[index+1] == '\\' {
			return index + 2
		}
		index++
	}
	return index
}

func plainTail(text string, maximum int) string {
	text = strings.TrimSpace(text)
	if len(text) <= maximum {
		return text
	}
	return text[len(text)-maximum:]
}
