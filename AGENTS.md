# Repository execution rules

- On Windows, never run `go test` directly. It executes test programs from
  changing temporary paths and can repeatedly trigger Windows Firewall prompts.
- Run Go verification through `build.cmd --host-only` or
  `node Tools/Build/go-tests.mjs`. That runner compiles and executes tests only
  from deterministic product-owned paths. On Windows that path is
  `%LOCALAPPDATA%\PCController\test-programs\go`, shared by every worktree.
- Use `go vet ./...` for the non-executing static-analysis pass. CI may use its
  native test runner on non-Windows hosts.
- This workstation also routes Go temporary output to
  `%LOCALAPPDATA%\PCController\go-noexec-temp`; files created there inherit a
  deny-execute ACL. Do not remove that guard to work around a direct-test
  failure—use the stable runner.
- Exercise real LAN discovery only with the named packaged
  `Tools/Controller/bin/controller.exe`; unit tests should use loopback or
  side-effect-free wire parsing.
