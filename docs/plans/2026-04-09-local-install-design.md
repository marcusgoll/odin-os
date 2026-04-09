# Local Odin Command Design

## Objective

Make `odin` a repeatable local command for development and dogfooding on this machine.

## Approaches

### 1. Copy the built binary into `~/.local/bin`

This is simple, but it creates a stale-binary problem after every rebuild.

### 2. Symlink `~/.local/bin/odin` to the repo build output

This is the recommended approach. A repeatable install target can build `bin/odin`, place a symlink in `~/.local/bin`, and let later `make build` runs update the command automatically.

### 3. Shell alias

This is not repeatable enough and does not belong to the repo.

## Design

Add a small local-install surface:

- `make install-local`
- `make uninstall-local`
- `scripts/dev/install-local.sh`
- `scripts/dev/uninstall-local.sh`

`install-local.sh` should:

1. resolve the repo root
2. resolve the source binary, defaulting to `bin/odin`
3. resolve the install directory, defaulting to `$HOME/.local/bin`
4. create the install directory
5. install a symlink named `odin`

`uninstall-local.sh` should remove the installed `odin` symlink from the same target directory.

The scripts should support overrides for testing:

- `ODIN_INSTALL_SOURCE`
- `ODIN_INSTALL_BIN_DIR`

## Testing

Add an integration test that:

1. creates a fake source binary
2. runs the install script with a temporary `HOME`
3. verifies the `odin` symlink points to the fake source
4. runs the uninstall script
5. verifies the link is removed

## Docs

Update `README.md` with a short local usage note so the expected operator flow becomes:

```bash
make build
make install-local
odin
```

