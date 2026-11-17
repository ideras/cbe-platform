#!/bin/sh
# Makes the image an exam's author built a workspace image: the student account, no way up, the folders the container's interface needs.
# The runner puts this script in the build of the last layer of every image with a base of its own and runs it there, as root, with the
# author's image as the file system; the layer then adds the agent, the labels, USER 1000:1000, WORKDIR /work and the agent as the entrypoint.
# It uses /bin/sh, sed, chmod, chown, mkdir, rm and find only (busybox has them all), so it works on any Linux base that has a shell.
set -eu

say() { echo "cbe-finalize: $*"; }
fail() { say "$*"; exit 1; }

[ -f /etc/passwd ] || fail "the image has no /etc/passwd: it is not a Linux system a student can log in to"
[ -f /etc/group ] || fail "the image has no /etc/group"

shell=/bin/sh
if [ -x /bin/bash ]; then shell=/bin/bash; elif [ -x /usr/bin/bash ]; then shell=/usr/bin/bash; fi

# 1. The student is uid 1000, gid 1000 and is called student (decision P0-30: the folder of the student's files is made for that uid). Whatever
# account or group has that number now (ubuntu in the Ubuntu images, node in the Node ones) is taken out, and so is any other called student.
old=$(sed -n 's/^\([^:]*\):[^:]*:1000:.*/\1/p' /etc/passwd | tr '\n' ' ')
oldg=$(sed -n 's/^\([^:]*\):[^:]*:1000:.*/\1/p' /etc/group | tr '\n' ' ')
sed -i -e '/^student:/d' -e '/^[^:]*:[^:]*:1000:/d' /etc/passwd
sed -i -e '/^student:/d' -e '/^[^:]*:[^:]*:1000:/d' /etc/group
if [ -f /etc/shadow ]; then
  sed -i '/^student:/d' /etc/shadow
  for name in $old; do sed -i "/^$name:/d" /etc/shadow; done
fi
[ -z "$old$oldg" ] || say "took out the account(s) and group(s) that had 1000: ${old}${oldg}"
echo "student:x:1000:1000:Student:/home/student:$shell" >> /etc/passwd
echo "student:x:1000:" >> /etc/group
if [ -f /etc/shadow ]; then echo "student:!:19000:0:99999:7:::" >> /etc/shadow; fi
mkdir -p /home/student
chown 1000:1000 /home/student
chmod 0700 /home/student

# 2. Nobody logs in as root: its password is locked. (A student has no root to log in to anyway: no capabilities, no new privileges.)
if [ -f /etc/shadow ]; then sed -i 's/^root:[^:]*:/root:*:/' /etc/shadow; fi

# 3. No sudo.
rm -f /usr/bin/sudo /usr/bin/sudoedit /usr/sbin/sudo /usr/sbin/visudo /etc/sudoers
rm -rf /etc/sudoers.d

# 4. No file with the setuid or setgid bit: it would be a way up.
if command -v find >/dev/null 2>&1; then
  count=$(find / -xdev \( -path /proc -o -path /sys \) -prune -o -type f \( -perm -4000 -o -perm -2000 \) -print 2>/dev/null | wc -l)
  find / -xdev \( -path /proc -o -path /sys \) -prune -o -type f \( -perm -4000 -o -perm -2000 \) -exec chmod ug-s {} + 2>/dev/null || true
  say "removed the setuid and setgid bit of $count file(s)"
else
  say "warning: there is no find in the image, so setuid files could not be looked for; the self test checks"
fi

# 5. The three places that are the container's interface exist: /work (the student's files are mounted there), /run/cbe (the agent's socket)
# and /tmp (a small tmpfs).
mkdir -p /work /run/cbe /tmp
chown 1000:1000 /work
chmod 1777 /tmp

say "student 1000:1000 ($shell), root locked, no sudo, no setuid files"
