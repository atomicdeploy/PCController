package artifacts

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type fakeUpdateService struct {
	journal                 string
	starts, stops           int
	unhealthy, startFailure bool
	children                []*fakeUpdateProcess
}
type fakeUpdateProcess struct {
	done chan struct{}
	once sync.Once
}

func (*fakeUpdateProcess) PID() int                                          { return 1234 }
func (p *fakeUpdateProcess) Stop() error                                     { p.once.Do(func() { close(p.done) }); return nil }
func (p *fakeUpdateProcess) Wait() error                                     { <-p.done; return errors.New("fixture service stopped") }
func (*fakeUpdateService) Validate(name, path string, pid int) error         { return nil }
func (s *fakeUpdateService) Stop(context.Context, string, string, int) error { s.stops++; return nil }
func (s *fakeUpdateService) Start(_ context.Context, name, path string) (updateProcess, error) {
	s.starts++
	if s.startFailure && s.starts == 1 {
		return nil, errors.New("fixture startup failure")
	}
	p := &fakeUpdateProcess{done: make(chan struct{})}
	s.children = append(s.children, p)
	if !s.unhealthy && s.starts == 1 {
		j, err := loadSelfUpdateJournal(s.journal)
		if err != nil {
			return nil, err
		}
		if err := writeHealthAcknowledgement(j.HealthPath, j.HealthToken); err != nil {
			return nil, err
		}
	}
	return p, nil
}

func TestServiceSelfUpdateReusesVerifiedReplacementHealthAndRollback(t *testing.T) {
	for _, scenario := range []string{"healthy", "health-timeout", "startup-failure"} {
		t.Run(scenario, func(t *testing.T) {
			directory := t.TempDir()
			current := filepath.Join(directory, "controller.exe")
			candidate := filepath.Join(directory, "candidate.exe")
			if err := copyTestExecutable(os.Args[0], current, nil); err != nil {
				t.Fatal(err)
			}
			if err := copyTestExecutable(os.Args[0], candidate, []byte("service candidate")); err != nil {
				t.Fatal(err)
			}
			original, _ := fileDigest(current)
			digest, _ := fileDigest(candidate)
			fake := &fakeUpdateService{unhealthy: scenario == "health-timeout", startFailure: scenario == "startup-failure"}
			previous := serviceUpdates
			serviceUpdates = fake
			defer func() {
				serviceUpdates = previous
				for _, p := range fake.children {
					p.Stop()
				}
			}()
			plan, err := PrepareSelfUpdateWithOptions(context.Background(), SelfUpdateOptions{
				CurrentPath: current, ArtifactPath: candidate, ExpectedSHA256: digest,
				WindowsService: "PCController", WorkingDirectory: directory, HealthTimeout: time.Second,
				Launcher: HelperLauncherFunc(func(context.Context, string, string) error { return nil }),
			})
			if err != nil {
				t.Fatal(err)
			}
			fake.journal = plan.JournalPath
			journal, _ := loadSelfUpdateJournal(plan.JournalPath)
			// An already-exited child is the outgoing PID; no process is killed.
			child := exec.Command(os.Args[0], "-test.run=^$")
			if err := child.Run(); err != nil {
				t.Fatal(err)
			}
			journal.ParentPID = child.Process.Pid
			if err := writeUpdateJSON(plan.JournalPath, journal); err != nil {
				t.Fatal(err)
			}
			err = RunSelfUpdateHelper(context.Background(), plan.JournalPath)
			if scenario == "startup-failure" {
				if err == nil {
					t.Fatal("startup failure concealed")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			journal, _ = loadSelfUpdateJournal(plan.JournalPath)
			wantDigest, wantStarts := digest, 1
			if scenario != "healthy" {
				wantDigest, wantStarts = original, 2
			}
			if err := verifyFileDigest(current, wantDigest); err != nil {
				t.Fatal(err)
			}
			if fake.stops != 1 || fake.starts != wantStarts {
				t.Fatalf("stops=%d starts=%d journal=%+v", fake.stops, fake.starts, journal)
			}
			if scenario == "healthy" && journal.State != "committed" {
				t.Fatalf("journal=%+v", journal)
			}
			if scenario == "health-timeout" && journal.State != "rolled-back" {
				t.Fatalf("journal=%+v", journal)
			}
		})
	}
}

func TestSelfUpdateRejectsOtherServiceNamesBeforeStaging(t *testing.T) {
	directory := t.TempDir()
	_, err := PrepareSelfUpdateWithOptions(context.Background(), SelfUpdateOptions{
		CurrentPath: filepath.Join(directory, "current.exe"), ArtifactPath: filepath.Join(directory, "new.exe"), WindowsService: "AnotherService",
	})
	if err == nil {
		t.Fatal("accepted another service")
	}
	entries, _ := os.ReadDir(directory)
	if len(entries) != 0 {
		t.Fatal("rejected service wrote staging files")
	}
}
