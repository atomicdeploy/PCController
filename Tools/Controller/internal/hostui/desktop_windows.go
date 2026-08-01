//go:build windows

package hostui

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
)

type registryWriter interface {
	Set(path, name, value string) error
}

type windowsRegistryWriter struct{}

func (windowsRegistryWriter) Set(path, name, value string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	return key.SetStringValue(name, value)
}

func ensurePlatformDesktopIntegration(
	options DesktopIntegrationOptions,
) (DesktopIntegrationStatus, error) {
	appID := strings.TrimSpace(options.AppID)
	if appID == "" {
		appID = "DRSDavidSoft.PCController"
	}
	displayName := strings.TrimSpace(options.DisplayName)
	if displayName == "" {
		displayName = "PCController"
	}
	executable, err := os.Executable()
	if err != nil {
		return DesktopIntegrationStatus{Supported: true, LastError: err.Error()}, err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return DesktopIntegrationStatus{Supported: true, LastError: err.Error()}, err
	}
	status := DesktopIntegrationStatus{Supported: true, Executable: executable}
	if err := ensureProtocolRegistry(windowsRegistryWriter{}, executable, appID, displayName); err != nil {
		status.LastError = err.Error()
		return status, err
	}
	status.ProtocolReady = true
	appData := os.Getenv("APPDATA")
	if strings.TrimSpace(appData) == "" {
		err := fmt.Errorf("APPDATA is unavailable")
		status.LastError = err.Error()
		return status, err
	}
	shortcut := filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", "PCController.lnk")
	if err := os.MkdirAll(filepath.Dir(shortcut), 0o755); err != nil {
		status.LastError = err.Error()
		return status, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := runPowerShell(
		ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive",
		"-EncodedCommand", encodedShortcutScript(executable, shortcut, appID),
	); err != nil {
		status.Shortcut = shortcut
		status.LastError = err.Error()
		return status, err
	}
	status.Shortcut, status.ShortcutReady = shortcut, true
	return status, nil
}

func ensureProtocolRegistry(
	registry registryWriter,
	executable, appID, displayName string,
) error {
	values := []struct{ path, name, value string }{
		{`Software\Classes\pccontroller`, "", "URL:PCController Protocol"},
		{`Software\Classes\pccontroller`, "URL Protocol", ""},
		{`Software\Classes\pccontroller\DefaultIcon`, "", quoteWindowsArgument(executable) + ",0"},
		{`Software\Classes\pccontroller\shell\open\command`, "", protocolCommand(executable)},
		{`Software\Classes\AppUserModelId\` + appID, "DisplayName", displayName},
		{`Software\Classes\AppUserModelId\` + appID, "IconUri", executable},
	}
	for _, value := range values {
		if err := registry.Set(value.path, value.name, value.value); err != nil {
			return fmt.Errorf("register desktop integration %s: %w", value.path, err)
		}
	}
	return nil
}

func protocolCommand(executable string) string {
	return quoteWindowsArgument(executable) + ` uri "%1"`
}

func quoteWindowsArgument(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

func encodedShortcutScript(executable, shortcut, appID string) string {
	encode := func(value string) string {
		return base64.StdEncoding.EncodeToString([]byte(value))
	}
	script := fmt.Sprintf(`
$ErrorActionPreference='Stop'
$exe=[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('%s'))
$shortcut=[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('%s'))
$appId=[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('%s'))
$shell=New-Object -ComObject WScript.Shell
$link=$shell.CreateShortcut($shortcut)
$link.TargetPath=$exe
$link.Arguments='tui'
$link.WorkingDirectory=[IO.Path]::GetDirectoryName($exe)
$link.IconLocation=$exe+',0'
$link.Save()
$code=@'
using System;
using System.Runtime.InteropServices;
[StructLayout(LayoutKind.Sequential, Pack=4)] public struct PROPERTYKEY { public Guid fmtid; public uint pid; }
[StructLayout(LayoutKind.Explicit)] public struct PROPVARIANT { [FieldOffset(0)] public ushort vt; [FieldOffset(8)] public IntPtr pointerValue; }
[ComImport, Guid("886D8EEB-8CF2-4446-8D02-CDBA1DBDCF99"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IPropertyStore { uint GetCount(out uint cProps); uint GetAt(uint iProp, out PROPERTYKEY pkey); uint GetValue(ref PROPERTYKEY key, out PROPVARIANT pv); uint SetValue(ref PROPERTYKEY key, ref PROPVARIANT pv); uint Commit(); }
public static class ShortcutAumid {
 [DllImport("shell32.dll", CharSet=CharSet.Unicode, PreserveSig=true)] static extern uint SHGetPropertyStoreFromParsingName(string path, IntPtr bind, uint flags, ref Guid iid, [Out, MarshalAs(UnmanagedType.Interface)] out IPropertyStore store);
 [DllImport("propsys.dll", CharSet=CharSet.Unicode)] static extern uint PSGetPropertyKeyFromName(string name, out PROPERTYKEY key);
 public static void Set(string path,string appId) { Guid iid=new Guid("886D8EEB-8CF2-4446-8D02-CDBA1DBDCF99"); IPropertyStore store; if(SHGetPropertyStoreFromParsingName(path,IntPtr.Zero,2,ref iid,out store)!=0) throw new Exception("open shortcut property store"); PROPERTYKEY key; if(PSGetPropertyKeyFromName("System.AppUserModel.ID",out key)!=0) throw new Exception("resolve AppUserModelID property"); PROPVARIANT pv=new PROPVARIANT(); pv.vt=31; pv.pointerValue=Marshal.StringToCoTaskMemUni(appId); try { if(store.SetValue(ref key,ref pv)!=0 || store.Commit()!=0) throw new Exception("write shortcut AppUserModelID"); } finally { Marshal.FreeCoTaskMem(pv.pointerValue); Marshal.ReleaseComObject(store); } }
}
'@
Add-Type -TypeDefinition $code
[ShortcutAumid]::Set($shortcut,$appId)
`, encode(executable), encode(shortcut), encode(appID))
	return base64.StdEncoding.EncodeToString(utf16LE(script))
}
