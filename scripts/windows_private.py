"""Conservative Windows DACL check for an already-open private input file."""
import ctypes
import os
from ctypes import wintypes
import msvcrt
import subprocess

advapi = ctypes.WinDLL('advapi32', use_last_error=True)
kernel = ctypes.WinDLL('kernel32', use_last_error=True)
ptr = ctypes.c_void_p


def api(dll, name, restype, *argtypes):
    function = getattr(dll, name)
    function.restype, function.argtypes = restype, argtypes
    return function


get_process = api(kernel, 'GetCurrentProcess', wintypes.HANDLE)
close_handle = api(kernel, 'CloseHandle', wintypes.BOOL, wintypes.HANDLE)
local_free = api(kernel, 'LocalFree', ptr, ptr)
open_token = api(advapi, 'OpenProcessToken', wintypes.BOOL, wintypes.HANDLE, wintypes.DWORD, ctypes.POINTER(wintypes.HANDLE))
get_token = api(advapi, 'GetTokenInformation', wintypes.BOOL, wintypes.HANDLE, wintypes.DWORD, ptr, wintypes.DWORD, ctypes.POINTER(wintypes.DWORD))
get_security = api(advapi, 'GetSecurityInfo', wintypes.DWORD, wintypes.HANDLE, wintypes.DWORD, wintypes.DWORD,
                   ctypes.POINTER(ptr), ctypes.POINTER(ptr), ctypes.POINTER(ptr), ctypes.POINTER(ptr), ctypes.POINTER(ptr))
get_ace = api(advapi, 'GetAce', wintypes.BOOL, ptr, wintypes.DWORD, ctypes.POINTER(ptr))
convert_sid = api(advapi, 'ConvertSidToStringSidW', wintypes.BOOL, ptr, ctypes.POINTER(wintypes.LPWSTR))
valid_sid = api(advapi, 'IsValidSid', wintypes.BOOL, ptr)
equal_sid = api(advapi, 'EqualSid', wintypes.BOOL, ptr, ptr)
well_known = api(advapi, 'IsWellKnownSid', wintypes.BOOL, ptr, wintypes.DWORD)


class ACL(ctypes.Structure):
    _fields_ = [('revision', ctypes.c_ubyte), ('reserved', ctypes.c_ubyte), ('size', wintypes.WORD),
                ('count', wintypes.WORD), ('reserved2', wintypes.WORD)]


class ACEHeader(ctypes.Structure):
    _fields_ = [('type', ctypes.c_ubyte), ('flags', ctypes.c_ubyte), ('size', wintypes.WORD)]


def sid_text(sid):
    """A SID pointer's text form, for diagnostics and explicit ACL grants."""
    if not sid or not valid_sid(sid):
        return '<invalid sid>'
    text = wintypes.LPWSTR()
    if not convert_sid(sid, ctypes.byref(text)):
        raise OSError(ctypes.get_last_error(), 'ConvertSidToStringSid failed')
    try:
        return text.value
    finally:
        local_free(ctypes.cast(text, ptr))


def user_sid():
    """The current account's SID as text, for explicit ACL grants."""
    token = wintypes.HANDLE()
    if not open_token(get_process(), 0x0008, ctypes.byref(token)):
        raise OSError(ctypes.get_last_error(), 'OpenProcessToken failed')
    try:
        size = wintypes.DWORD()
        get_token(token, 1, None, 0, ctypes.byref(size))
        if not size.value:
            raise OSError(ctypes.get_last_error(), 'GetTokenInformation size failed')
        user_info = ctypes.create_string_buffer(size.value)
        if not get_token(token, 1, user_info, size, ctypes.byref(size)):
            raise OSError(ctypes.get_last_error(), 'GetTokenInformation failed')
        return sid_text(ctypes.cast(user_info, ctypes.POINTER(ptr)).contents)
    finally:
        close_handle(token)


SDDL_REVISION_1 = 1
DACL_SECURITY_INFORMATION = 0x00000004


def _verify_private(path):
    with open(path, 'rb') as handle:
        check_private(handle)


def _set_protected_dacl(path):
    """Give path a DACL holding exactly one full-access entry, for the current account, with inheritance off.

    This is deterministic in a way icacls is not: it replaces the whole access list instead of editing the one
    the image's default template produced. The entry points are resolved lazily so this module still imports
    if an old system lacks them."""
    convert = api(advapi, 'ConvertStringSecurityDescriptorToSecurityDescriptorW', wintypes.BOOL,
                  wintypes.LPCWSTR, wintypes.DWORD, ctypes.POINTER(ptr), ctypes.POINTER(wintypes.DWORD))
    apply = api(advapi, 'SetFileSecurityW', wintypes.BOOL, wintypes.LPCWSTR, wintypes.DWORD, ptr)
    descriptor, size = ptr(), wintypes.DWORD()
    if not convert('D:P(A;;FA;;;' + user_sid() + ')', SDDL_REVISION_1, ctypes.byref(descriptor), ctypes.byref(size)):
        raise OSError(ctypes.get_last_error(), 'ConvertStringSecurityDescriptorToSecurityDescriptor failed')
    try:
        if not apply(os.fspath(path), DACL_SECURITY_INFORMATION, descriptor):
            raise OSError(ctypes.get_last_error(), 'SetFileSecurity failed')
    finally:
        local_free(descriptor)


def restrict_private(path):
    """Replace a private input's inherited ACL with an explicit one for the current account alone, then verify it.

    A file that merely inherits a profile or temporary directory's ACL can carry extra access-granting
    entries (for example application-container SIDs) on Windows 11, which the strict private-input policy
    rejects. Private inputs are created and then restricted, so policy holds on every Windows image. The
    result is checked with the same rule the server applies, so a fixture that is still not private fails
    here with the offending SID instead of surfacing later as an opaque server startup error."""
    try:
        subprocess.run(['icacls', os.fspath(path), '/inheritance:r', '/grant:r', '*' + user_sid() + ':F'],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
    except subprocess.CalledProcessError as failure:
        # Report as OSError so every caller's handler covers it; CalledProcessError is not one.
        detail = (failure.stderr or b'').decode('utf-8', 'replace').strip()
        raise OSError('icacls could not restrict %s (exit %d): %s' % (path, failure.returncode, detail))
    try:
        _verify_private(path)
    except ValueError as rejected:
        # Keep whatever the image's ACL template left behind from deciding the outcome.
        _set_protected_dacl(path)
        try:
            _verify_private(path)
        except ValueError:
            raise ValueError('%s (after replacing the access list)' % rejected)


def check_private(file):
    token, owner, dacl, descriptor = wintypes.HANDLE(), ptr(), ptr(), ptr()
    if not open_token(get_process(), 0x0008, ctypes.byref(token)):
        raise OSError(ctypes.get_last_error(), 'OpenProcessToken failed')
    try:
        size = wintypes.DWORD()
        get_token(token, 1, None, 0, ctypes.byref(size))
        if not size.value:
            raise OSError(ctypes.get_last_error(), 'GetTokenInformation size failed')
        user_info = ctypes.create_string_buffer(size.value)
        if not get_token(token, 1, user_info, size, ctypes.byref(size)):
            raise OSError(ctypes.get_last_error(), 'GetTokenInformation failed')
        user_sid = ctypes.cast(user_info, ctypes.POINTER(ptr)).contents
        handle = wintypes.HANDLE(msvcrt.get_osfhandle(file.fileno()))
        error = get_security(handle, 1, 0x00000001 | 0x00000004, ctypes.byref(owner), None,
                             ctypes.byref(dacl), None, ctypes.byref(descriptor))
        if error:
            raise OSError(error, 'GetSecurityInfo failed')
        try:
            def trusted(sid):
                return bool(sid and valid_sid(sid) and (equal_sid(sid, user_sid) or well_known(sid, 22) or well_known(sid, 26)))
            # SIDs are not secrets, so a rejection names the offending entry: a hosted failure then shows exactly
            # which account or ACE the policy refused instead of a generic verdict.
            if not trusted(owner):
                raise ValueError('Windows input ACL must be private (file is owned by %s)' % sid_text(owner))
            if not dacl:
                raise ValueError('Windows input ACL must be private (no access list)')
            acl = ctypes.cast(dacl, ctypes.POINTER(ACL)).contents
            for index in range(acl.count):
                ace = ptr()
                if not get_ace(dacl, index, ctypes.byref(ace)) or not ace:
                    raise ValueError('Cannot inspect Windows input ACL')
                header = ctypes.cast(ace, ctypes.POINTER(ACEHeader)).contents
                if header.flags & 0x08 or header.type == 1:
                    continue
                sid = ptr(ace.value + 8)
                if header.type != 0:
                    raise ValueError('Windows input ACL must be private (access entry %d has unsupported type %d for %s)'
                                     % (index, header.type, sid_text(sid)))
                if header.size < 16:
                    raise ValueError('Windows input ACL must be private (malformed access entry %d)' % index)
                if not trusted(sid):
                    raise ValueError('Windows input ACL must be private (access entry %d grants %s)'
                                     % (index, sid_text(sid)))
        finally:
            if descriptor:
                local_free(descriptor)
    finally:
        close_handle(token)
