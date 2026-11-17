# Installing the CBE Portal server on Linux

The server is installed in two steps that are kept apart on purpose:

1. **The installer** (`cbe-portal-VERSION-linux-ARCH.run`) puts the program files in one folder, `/opt/cbe-portal`. It changes nothing else, so it can be run again, and undone, safely.
2. **The configurator** (`cbe-portalctl configure`) makes the machine a server: the accounts, the folders, the XFS volume for workspace exams, the services. It does what a configuration file says (or what a few questions
   answer), it checks first and shows what it will do, and it can be run again: what is done is left alone.

```
/opt/cbe-portal/                       the program: replaced by an upgrade, removed by the uninstaller. Never holds exams.
  bin/   cbe-portal-server  cbe-runner  cbe-quota-helperd  cbe-portalctl  examc
  etc/   cbe-portal.conf (the answers)   runner.json (the runner's settings)        kept by an upgrade
  lib/systemd/   the three units (made by the configurator), linked into the folders systemd reads
  share/ docs/  examples/lab-listas
  VERSION  ARCH  uninstall.sh

DATA_DIR (default /var/lib/cbe-portal)  the data: nothing of it is touched by an upgrade or by the uninstaller
  server/   the database, the screenshots, the proctor's token, workspace bundles and snapshots (the server account's)
  ipc/ run/ runner/ grading/   the runner's folders (workspace exams)
  work/     the XFS volume: one size-limited folder for each student's workspace
  cbe-portal-work.img          the file that holds that volume (a sparse file: it takes disk as it is used)
```

Only the systemd units are outside: `/etc/systemd/system/cbe-server.service` and `cbe-quota-helper.service` are links to `/opt/cbe-portal/lib/systemd/`, and the runner's unit is linked in the `cbe-runner` account's own user units.

## What the machine needs

| | Question exams only (`WORKSPACE_ENABLED=no`) | Workspace exams (`WORKSPACE_ENABLED=yes`) |
|---|---|---|
| System | Linux with systemd, x86-64 or arm64 | the same, with **cgroup v2** (the default on current systems) |
| Programs | none | **Podman 4 or later**, `uidmap` (`newuidmap`), `xfsprogs` (`mkfs.xfs`) |
| Disk | a few hundred MB for the data | room for the volume (64 GiB by default, used as it is needed), and for images (a C++ image is under 1 GB, a Python image with its libraries several) |

On Debian and Ubuntu: `sudo apt install podman uidmap xfsprogs`. `cbe-portalctl doctor` says what is missing.

## Installing

Download the installer for your machine from the release page, check it, and run it as root:

```sh
sha256sum -c cbe-portal-0.17.0-linux-x64.run.sha256
sudo sh cbe-portal-0.17.0-linux-x64.run                    # asks whether to configure now
sudo sh cbe-portal-0.17.0-linux-x64.run -- --config cbe.conf --yes     # unattended: the options after -- are the installer's
```

| Installer option | |
|---|---|
| `--prefix DIR` | Where the program files go. Default `/opt/cbe-portal`. |
| `--config FILE` | Configure the server from this file once the files are in place. |
| `--yes` | Do not ask before changing the machine. |
| `--no-configure` | Only put the files in place (`cbe-portalctl configure` is run later). |
| `--no-link` | Do not link `cbe-portalctl` and `examc` into `/usr/local/bin`. |

## Configuring

```sh
sudo cbe-portalctl configure              # asks a few questions, shows the plan, asks "go ahead?"
sudo cbe-portalctl configure --config cbe.conf --yes      # unattended
sudo cbe-portalctl configure --config cbe.conf --dry-run  # says what it would do, changes nothing (no root needed)
cbe-portalctl sample-config > cbe.conf    # a documented file to start from
```

The configuration file is parsed, never run: one `KEY=VALUE` on a line, `#` for comments, `${NAME}` for the value of another key. A key that does not exist, a value that is not what its key takes, or keys that
contradict each other, are reported with the line, and nothing is changed. Keys left out take their defaults.

```bash
WORKSPACE_ENABLED=yes
DATA_DIR=/data/cbe-platform
DATABASE_FILEPATH=/data/cbe-platform/server/cbe-platform.db
CREATE_XFS_VOLUME=yes
XFS_VOLUME_FILE=/data/cbe-platform/cbe-portal-work.img
XFS_VOLUME_SIZE=64G
```

### The keys

| Key | Default | |
|---|---|---|
| `INSTALL_DIR` | `/opt/cbe-portal` | Where the program is (the installer sets it; left out, it is where `cbe-portalctl` is). |
| `DATA_DIR` | `/var/lib/cbe-portal` | Everything the server keeps. Not under `/home` (the services run with `ProtectHome`) and not inside `INSTALL_DIR`. |
| `WORKSPACE_ENABLED` | `no` | `yes`: serve workspace exams. `no`: question exams only; none of the keys below that are marked *workspace* matter. |
| `BIND_ADDRESS` | `0.0.0.0:8080` | host:port the exam server listens on. |
| `SERVER_USER` | `cbe-server` | The account the server runs as (made, with no login). |
| `DATABASE_FILEPATH` | `${DATA_DIR}/server/cbe-platform.db` | The SQLite database; the server makes and upgrades it by itself. **In a folder of its own under `DATA_DIR`**: the server writes next to the file (its journal), and `DATA_DIR` itself holds the runner's folders. |
| `SCREENSHOTS_DIR` | `${DATA_DIR}/server/screenshots` | |
| `ADMIN_TOKEN_FILE` | `${DATA_DIR}/server/admin-token` | The proctor's token. A random one is made when the file does not exist; one that is there is kept. |
| `WORKSPACE_DIR` | `${DATA_DIR}/server/workspace` | *workspace.* Bundles and snapshots. |
| `SERVER_EXTRA_ARGS` | | More flags for the server (`cbe-portal-server -h`), for example `-workspace-retention=48h`. |
| `START_SERVICES` | `yes` | Start the services and enable them at boot. `no`: only prepare them. |
| `RUNNER_USER` | `cbe-runner` | *workspace.* The account that holds the container runtime (rootless Podman). |
| `IPC_GROUP` | `cbe-ipc` | *workspace.* The group that lets the server reach the runner's socket and nothing else of the runner's. |
| `WORK_DIR` | `${DATA_DIR}/work` | *workspace.* Where the XFS volume is mounted. |
| `CREATE_XFS_VOLUME` | `no` | *workspace.* `yes`: make the volume (a file, or `XFS_DEVICE`) and mount it. `no`: `WORK_DIR` must already be an XFS mount with project quotas (`prjquota`). |
| `XFS_VOLUME_FILE` | `${DATA_DIR}/cbe-portal-work.img` | *workspace.* The file that holds the volume. Not inside `WORK_DIR`, which it is mounted on. |
| `XFS_VOLUME_SIZE` | `64G` | *workspace.* A number and `M`, `G` or `T`. Sparse: it takes disk as it is used. 40 students at the default 100 MiB need 4 GiB. |
| `XFS_DEVICE` | | *workspace.* A spare partition to format instead of a file, for example `/dev/sdb1`. |
| `XFS_DEVICE_ERASE` | `no` | *workspace.* `yes` lets `XFS_DEVICE` be formatted. Nothing is ever formatted without it. |
| `HOST_MEMORY_MB` | `0` | *workspace.* The most memory the students' containers may add up to. `0` is 70% of the machine's memory. |
| `CONTAINER_STORAGE_DIR` | | *workspace.* Where the container runtime keeps its images (default: under the runner's home). Images already there are not moved. |

### What it does, and what it will not do

In this order, each step checking first whether it is already done: the folders, the accounts (`cbe-server`; for workspace exams `cbe-runner` and the group `cbe-ipc`), the proctor's token, the runner's folders; for workspace
exams the XFS volume (made, added to `/etc/fstab` once with the old file kept as `/etc/fstab.bak-cbe`, and mounted), lingering for the runner, the delegation of control groups and the limits of the containers' slice, the
runtime's socket, the runner's settings; then the units (written in `/opt/cbe-portal/lib/systemd`, linked, enabled), and the services started.

It will not:

- **format anything that has something on it**: a volume file that is there and is not XFS, a device that is mounted, or a file with no file system that this run did not make, are problems that stop it;
- **change a folder that belongs to someone else** (a database folder owned by another account is a problem, not a `chown`);
- **stop a class**: a service that runs is not restarted by `configure` (`cbe-portalctl restart` is where that is decided), and the runner's user manager is not restarted while containers run;
- **overwrite the runner's settings you changed**: what the configuration decides (the folders, the sockets, the memory budget) is put right and the rest (limits, images) is kept.

A file at a place a link must go (a unit from an older installation) is kept beside it as `NAME.before-cbe-portalctl`.

### After it

```sh
sudo cat /data/cbe-platform/server/admin-token     # the proctor's token: give it to the proctor application (Server connection) with the server's address
cbe-portalctl status                               # the services, the volume, how many students are in their workspaces
sudo journalctl -u cbe-server -f                   # the server's log (the quota helper: -u cbe-quota-helper)
sudo journalctl _SYSTEMD_USER_UNIT=cbe-runner.service -f      # the runner's log
```

## Upgrading

Run the installer of the new version. It replaces the programs, keeps `etc/` and the data, writes the units anew, and says that the services still run the old version: **when no class is working**,
`sudo cbe-portalctl restart` (it refuses while students are in their workspaces, unless `--force`; the students' containers and files are safe, and the application reconnects). The server upgrades its own
database on its first start (and keeps a copy of the old one).

## Uninstalling

```sh
sudo /opt/cbe-portal/uninstall.sh               # the services and the program files go; THE DATA STAYS
sudo /opt/cbe-portal/uninstall.sh --remove-data # and all the data: the database, screenshots, the students' folders, the volume and the accounts (it asks you to type remove-data)
```

When the data stays, the answers are kept with it in `DATA_DIR/cbe-portal.conf.last`: a new installation of the same server is `cbe-portalctl configure --config DATA_DIR/cbe-portal.conf.last`, and finds its data.

## A server for question exams only, behind a proxy

`WORKSPACE_ENABLED=no` needs no Podman and no volume. For a server on the internet, keep it on the loopback address and let a reverse proxy hold the certificate:

```bash
WORKSPACE_ENABLED=no
BIND_ADDRESS=127.0.0.1:8081
```

The server takes the student's address from the last `X-Forwarded-For` entry only when the connection comes from the loopback address, so the proxy must be on the same machine.

## Images for workspace exams

The installer holds no course image, and the configurator makes none. An exam's own image is built from its package (`image/Containerfile`) when the exam is published (`docs/workspace/images.md`). Until an exam can name any
base image (planned), the base of today's packages, `cbe/cpp20`, is built once on the container host with `images/build.sh cpp20` from the source tree (`images/README.md`).

## Backups

Copy, with the server stopped or from a snapshot: the database (`DATABASE_FILEPATH`, with its `-wal` and `-shm` files), `SCREENSHOTS_DIR`, `WORKSPACE_DIR`, and `/opt/cbe-portal/etc/cbe-portal.conf`. The students' files are kept in
the workspace snapshots under `WORKSPACE_DIR`; the volume file is a disk image of the live workspaces and is not a backup target (it is sparse: copy it with `cp --sparse=always`, if at all).

## When something is wrong

| | |
|---|---|
| `cbe-portalctl doctor` | Checks the machine for the configuration without changing anything: systemd, the programs, Podman and its version, `newuidmap`, cgroup v2, room for the volume. |
| `... belongs to 1000:1000, not to cbe-server` | A folder that is there was not made by this configuration and is left alone: give it to the account (`chown`) or choose another folder. |
| `... has no range for cbe-runner` | The runner's account has no subordinate user ids (`/etc/subuid`, `/etc/subgid`), which rootless containers need: `usermod --add-subuids 200000-265535 --add-subgids 200000-265535 cbe-runner` with a free range. |
| `... the user manager has to be restarted to hold the limits` | The limits could not be applied without stopping the containers that run: restart the user manager (or reboot) when no class is working, and run `configure` again. |
| the volume is not mounted after a reboot | `findmnt /data/cbe-platform/work`; the line in `/etc/fstab` has `nofail` and waits for the file's folder (`x-systemd.requires-mounts-for`). `sudo mount WORK_DIR` mounts it. |
