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
        sid = ctypes.cast(user_info, ctypes.POINTER(ptr)).contents
        text = wintypes.LPWSTR()
        if not convert_sid(sid, ctypes.byref(text)):
            raise OSError(ctypes.get_last_error(), 'ConvertSidToStringSid failed')
        try:
            return text.value
        finally:
            local_free(ctypes.cast(text, ptr))
    finally:
        close_handle(token)


def restrict_private(path):
    """Replace a private input's inherited ACL with an explicit one for the current account alone.

    A file that merely inherits a profile or temporary directory's ACL can carry extra access-granting
    entries (for example application-container SIDs) on Windows 11, which the strict private-input policy
    rejects. Private inputs are created and then restricted, so policy holds on every Windows image."""
    subprocess.run(['icacls', os.fspath(path), '/inheritance:r', '/grant:r', '*' + user_sid() + ':F'],
                   check=True, stdout=subprocess.DEVNULL)


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
            if not trusted(owner) or not dacl:
                raise ValueError('Windows input ACL must be private')
            acl = ctypes.cast(dacl, ctypes.POINTER(ACL)).contents
            for index in range(acl.count):
                ace = ptr()
                if not get_ace(dacl, index, ctypes.byref(ace)) or not ace:
                    raise ValueError('Cannot inspect Windows input ACL')
                header = ctypes.cast(ace, ctypes.POINTER(ACEHeader)).contents
                if header.flags & 0x08 or header.type == 1:
                    continue
                if header.type != 0 or header.size < 16 or not trusted(ptr(ace.value + 8)):
                    raise ValueError('Windows input ACL must be private')
        finally:
            if descriptor:
                local_free(descriptor)
    finally:
        close_handle(token)
