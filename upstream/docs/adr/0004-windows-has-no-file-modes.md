# 0004: Windows has no file modes

`load_login` refuses a login file that group or others can read (`chmod 600`). On Windows it skips that check. `config_set` also asks for mode `0o700` on the folder and `0o600` on the file; Windows ignores both.

## Why

Windows has no POSIX mode bits. Python reports every file as `0o666`, so the check refused every login file, and `handle-mcp login <store> --from-env` could never work on Windows (found by the first Windows CI run, #91). Checking the real Windows ACL needs a new dependency (pywin32) for a fallback few people use.

## Consequences

- On Windows the file is only as private as its folder. The default `~/.config/handle-mcp` is in the user profile, which only that user (and admins) can read. A `HANDLE_CONFIG_DIR` in a shared folder gets no warning.
- The keychain (Windows Credential Locker) stays the recommended login on Windows. The file is a fallback, as on every OS.
- macOS and Linux keep the check.
