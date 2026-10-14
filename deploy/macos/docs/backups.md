# Backups

Read this before you rely on the install: the backup job stays off until `AGE_RECIPIENT`
is set. Accounts and finished-game history are the data worth protecting.

## 1. Make a key pair, off the Mac

On your laptop, not the server:

```bash
brew install age
age-keygen -o key.txt          # prints "Public key: age1..."
age-keygen -y key.txt          # prints the public key again; proves the file works
```

`key.txt` is the **only copy** of the private key. Store it in a password manager or
offline. Without it the backups cannot be read, by anyone. Put only the `age1...` public key
in the env file as `AGE_RECIPIENT`, then re-run `sudo deploy/macos/install.sh`.

## 2. What the job does

Nightly at 03:15, as `_cribbager`: `pg_dump --no-owner --no-acl | gzip | age -r <public key>`
into `/usr/local/cribbager/backups/cribbager-<UTC>.sql.gz.age` (mode 600). It keeps the
newest 7 files plus anything under 30 days old (`BACKUP_KEEP_MIN`, `BACKUP_KEEP_DAYS`) and
touches `backups/.last-success`. Run one now:

```bash
sudo -u _cribbager /usr/local/libexec/cribbager/backup.sh && ls -l /usr/local/cribbager/backups
```

## 3. Restore drill

Do this once after setup, then every few months. It restores into a scratch database and
leaves production alone.

```bash
B=$(sudo sh -c 'ls -t /usr/local/cribbager/backups/cribbager-*.age | head -1')
install -m 600 /dev/null /tmp/age-key.txt && cat > /tmp/age-key.txt   # paste key.txt, Ctrl-D
PGB=/opt/homebrew/opt/postgresql@16/bin
PSQL="sudo -u _cribbager $PGB/psql -X -h /usr/local/cribbager/run -U postgres -v ON_ERROR_STOP=1"
$PSQL -d postgres -c 'CREATE DATABASE restore_test OWNER cribbager'
{ echo 'SET ROLE cribbager;'; sudo cat "$B" | /opt/homebrew/bin/age -d -i /tmp/age-key.txt | gunzip; } | $PSQL -d restore_test
$PSQL -d restore_test -c '\dt' -c 'SELECT count(*) FROM users'
$PSQL -d postgres -c 'DROP DATABASE restore_test'
rm -P /tmp/age-key.txt
```

`SET ROLE` makes the app role own the restored tables; the dump has no ownership statements.

**Real restore:** stop the app, replace the database, restore, start the app. Reuse `B`,
`PSQL` and the key file from above, but point them at `cribbager`:

```bash
sudo launchctl bootout system/org.cribbager.app
$PSQL -d postgres -c 'DROP DATABASE cribbager' -c 'CREATE DATABASE cribbager OWNER cribbager'
{ echo 'SET ROLE cribbager;'; sudo cat "$B" | /opt/homebrew/bin/age -d -i /tmp/age-key.txt | gunzip; } | $PSQL -d cribbager
sudo launchctl bootstrap system /Library/LaunchDaemons/org.cribbager.app.plist
```

On a rebuilt Mac, run `install.sh` first (it creates the empty role and database).

## 4. Copy backups off the machine

The disk is a single point of failure, and a compromised `_cribbager` could delete local
backups. The files are already encrypted, so any destination is fine. Pick one:

- Set `BACKUP_POST_CMD` in the env file; it runs after each successful backup with
  `$BACKUP_FILE` set, for example an `rsync` or `rclone` call to a host or bucket you
  control. Keep its credentials under `/usr/local/cribbager/home`.
- Point Time Machine or another tool at `/usr/local/cribbager/backups`, never at `pgdata`
  (a file copy of a running database is not restorable; install.sh excludes it).
- Pull the directory from another machine.

Alert yourself if `backups/.last-success` is more than about 26 hours old.
