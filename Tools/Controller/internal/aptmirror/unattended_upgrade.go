package aptmirror

import "fmt"

const (
	UnattendedUpgradeRealPath   = "/usr/bin/unattended-upgrade"
	UnattendedUpgradeShimPath   = "/opt/pccontroller/libexec/unattended-upgrade"
	UnattendedUpgradeDropInPath = "/etc/systemd/system/apt-daily-upgrade.service.d/50-pccontroller-origin-cache.conf"
)

// This exact affected constructor is shared by the reviewed Ubuntu 24.04.4
// apt_pkg 2.8.3 and Ubuntu 26.04 apt_pkg 3.2.0 hosts. Its SHA-256 is
// 3abb1ceff3af2e4f5b42f45c9e16754632c8bfd3db062b7e1f9041328d220f9f.
const unattendedUpgradeAffectedOriginConstructor = `    def __init__(self, pkg: Package, packagefile: apt_pkg.PackageFile) -> None:
        self.archive = packagefile.archive
        self.component = packagefile.component
        self.label = packagefile.label
        self.origin = packagefile.origin
        self.codename = packagefile.codename
        self.site = packagefile.site
        self.not_automatic = packagefile.not_automatic
        # check the trust
        indexfile = pkg._pcache._list.find_index(packagefile)
        if indexfile and indexfile.is_trusted:
            self.trusted = True
        else:
            self.trusted = False
`

// UnattendedUpgradeShim returns a same-process launcher. runpy is deliberate:
// exec would discard the narrowly scoped monkey patch, while a child process
// would change the signal and lifecycle behavior expected by apt.systemd.daily.
func UnattendedUpgradeShim() []byte {
	return unattendedUpgradeShim(UnattendedUpgradeRealPath, "", 0)
}

func unattendedUpgradeShim(realProgram, testModuleRoot string, expectedUID int) []byte {
	return []byte(fmt.Sprintf(`#!/usr/bin/python3 -I
# Generated and transactionally owned by PCController. Do not edit.
import inspect
import ast
import hashlib
import os
import runpy
import stat
import sys
import textwrap
import types

_REAL_PROGRAM = %q
_TEST_MODULE_ROOT = %q
_EXPECTED_UID = %d
_AFFECTED_CONSTRUCTORS = (%q,)
_CACHE_ATTRIBUTE = "_pccontroller_origin_state_v1"

if _TEST_MODULE_ROOT:
    # This constant is empty in production output. It exists only so the Go
    # test fixture can exercise the wrapper under Python isolated mode.
    sys.path.insert(0, _TEST_MODULE_ROOT)

import apt
import apt.package as apt_package

_ORIGINAL = apt_package.Origin.__init__


def _fail(detail):
    raise SystemExit("PCController unattended-upgrade compatibility check failed: " + detail)


def _validate_real_program():
    try:
        info = os.lstat(_REAL_PROGRAM)
    except OSError as error:
        _fail("cannot inspect the distro program: " + str(error))
    if (not stat.S_ISREG(info.st_mode) or info.st_uid != _EXPECTED_UID or
            info.st_mode & (stat.S_IWGRP | stat.S_IWOTH)):
        _fail("the distro program is not a root-owned, non-writable regular file")


def _classify_constructor():
    if (_ORIGINAL.__module__ != "apt.package" or
            _ORIGINAL.__qualname__ != "Origin.__init__"):
        _fail("apt.package.Origin.__init__ has an unexpected identity")
    try:
        actual = inspect.getsource(_ORIGINAL)
    except (OSError, TypeError) as error:
        _fail("cannot inspect apt.package.Origin.__init__: " + str(error))
    digest = hashlib.sha256(actual.encode()).hexdigest()
    if actual in _AFFECTED_CONSTRUCTORS:
        return ("affected", digest)
    try:
        tree = ast.parse(textwrap.dedent(actual))
    except SyntaxError as error:
        _fail("cannot parse apt.package.Origin.__init__: " + str(error))
    for node in ast.walk(tree):
        if (isinstance(node, ast.Call) and isinstance(node.func, ast.Attribute) and
                node.func.attr == "find_index"):
            _fail("unknown affected apt.package.Origin.__init__ " + digest + "; reprovision after review")
    return ("passthrough", digest)


def _packagefile_key(packagefile):
    return (int(packagefile.id), str(packagefile.filename))


def _cached_origin_init(self, pkg, packagefile):
    cache_owner = pkg._pcache
    states = getattr(cache_owner, _CACHE_ATTRIBUTE, None)
    if states is None:
        states = {}
        try:
            setattr(cache_owner, _CACHE_ATTRIBUTE, states)
        except Exception as error:
            _fail("cannot scope the Origin cache to the current apt.Cache: " + str(error))
    key = _packagefile_key(packagefile)
    state = states.get(key)
    if state is None:
        # Trust and every other field are still derived by the reviewed
        # upstream constructor. We only memoize its immutable result.
        _ORIGINAL(self, pkg, packagefile)
        states[key] = self.__dict__.copy()
        return
    self.__dict__.update(state)


def _new_origin(constructor, pkg, packagefile):
    value = object.__new__(apt_package.Origin)
    constructor(value, pkg, packagefile)
    return value


def _self_test():
    global _ORIGINAL
    cache = apt.Cache()
    packagefiles = list(cache._cache.file_list)
    if not packagefiles:
        _fail("apt.Cache exposed no PackageFiles")
    pkg = types.SimpleNamespace(_pcache=cache)
    expected = {}
    upstream = _ORIGINAL
    for packagefile in packagefiles:
        key = _packagefile_key(packagefile)
        if key in expected:
            _fail("apt.Cache exposed duplicate PackageFile identities")
        expected[key] = _new_origin(upstream, pkg, packagefile).__dict__.copy()

    calls = 0

    def counted(self, current_pkg, packagefile):
        nonlocal calls
        calls += 1
        upstream(self, current_pkg, packagefile)

    setattr(cache, _CACHE_ATTRIBUTE, {})
    _ORIGINAL = counted
    try:
        for packagefile in packagefiles:
            key = _packagefile_key(packagefile)
            first = apt_package.Origin(pkg, packagefile)
            second = apt_package.Origin(pkg, packagefile)
            if first.__dict__ != expected[key] or second.__dict__ != expected[key]:
                _fail("cached Origin state differs from upstream for " + repr(key))
    finally:
        _ORIGINAL = upstream

    states = getattr(cache, _CACHE_ATTRIBUTE, None)
    if calls != len(packagefiles) or states is None or set(states) != set(expected):
        _fail("Origin cache did not memoize exactly once per PackageFile")
    print("PCController unattended-upgrade compatibility validated %%d PackageFiles" %% len(packagefiles))


_validate_real_program()
_MODE, _SOURCE_SHA256 = _classify_constructor()
if _MODE == "affected":
    apt_package.Origin.__init__ = _cached_origin_init

if len(sys.argv) == 2 and sys.argv[1] == "--pccontroller-self-test":
    if _MODE == "affected":
        _self_test()
    else:
        print("PCController unattended-upgrade compatibility passthrough; upstream Origin has no find_index call; sha256=" + _SOURCE_SHA256)
else:
    # The distro program remains in this process, retaining its argv, signals,
    # locks, logging and systemd service identity.
    runpy.run_path(_REAL_PROGRAM, run_name="__main__")
`, realProgram, testModuleRoot, expectedUID, unattendedUpgradeAffectedOriginConstructor))
}

func UnattendedUpgradeSystemdDropIn() []byte {
	return []byte(`[Service]
Environment="PATH=/opt/pccontroller/libexec:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
`)
}
