# ct2stimer

[![CI](https://github.com/HQJaTu/ct2stimer/actions/workflows/ci.yml/badge.svg)](https://github.com/HQJaTu/ct2stimer/actions/workflows/ci.yml)

Convert crontab to systemd timer

```bash
ubuntu@ubuntu-xenial:~/src/github.com/dtan4/ct2stimer$ sudo ct2stimer -f sample.cron --reload
ubuntu@ubuntu-xenial:~/src/github.com/dtan4/ct2stimer$ systemctl list-timers
NEXT                         LEFT                   LAST PASSED UNIT                         ACTIVATES
Fri 2017-01-20 07:50:00 UTC  4min 16s left          n/a  n/a    cron-77e2fb273c45.timer      cron-77e2fb273c45.service
Fri 2017-01-20 07:56:01 UTC  10min left             n/a  n/a    systemd-tmpfiles-clean.timer systemd-tmpfiles-clean.service
Fri 2017-01-20 08:00:00 UTC  14min left             n/a  n/a    cron-1b33d99b7dda.timer      cron-1b33d99b7dda.service
Fri 2017-01-20 10:00:00 UTC  2h 14min left          n/a  n/a    cron-b60fe106ef63.timer      cron-b60fe106ef63.service
Fri 2017-01-20 12:16:09 UTC  4h 30min left          n/a  n/a    snapd.refresh.timer          snapd.refresh.service
Fri 2017-01-20 19:11:59 UTC  11h left               n/a  n/a    apt-daily.timer              apt-daily.service
Wed 2017-02-01 00:00:00 UTC  1 weeks 4 days left    n/a  n/a    cron-fcd6d8377d9d.timer      cron-fcd6d8377d9d.service
Sat 2017-12-02 01:23:00 UTC  10 months 11 days left n/a  n/a    cron-d3c507cb2439.timer      cron-d3c507cb2439.service

8 timers listed.
Pass --all to see loaded but inactive timers, too.
```

## Installation

### Manually

There mostly isn't an installation.

`cp bin/ct2stimer /usr/local/bin/`

Is the closest that I can think of.

### Packaging

TBD

## Usage

ct2stimer reads the crontab file given by the `-f FILE` flag. The flag is required;
running without it prints usage and exits.
(It deliberately does **not** read `/etc/crontab` by default — that is a system crontab with a different format.)

Blank lines, comments and environment-variable assignments (`SHELL=`, `PATH=`, `MAILTO=`, ...) are ignored,
and both standard 5-field specs and `@` descriptors (`@daily`, `@hourly`, ...) are supported.

systemd unit file are saved at `/run/systemd/system` by default. This is important!
On a typical Linux `/run/` is a tmpfs (a RAMdisk) that won't survive a reboot.

You can specify save directory with `-o OUTDIR` flag.
To persist a reboot, suggested output directories are: 
- system as root: `/etc/systemd/system`
- user as non-root: `~/.config/systemd/user/`
  - Enable [lingering](https://www.freedesktop.org/software/systemd/man/latest/loginctl.html), if you want timers to trigger also when not logged in

```bash
$ ct2stimer -f sample.cron
$ ct2stimer -f sample.cron -o unitfiles
```

### Reload systemd and start all timers automatically

If `--reload` is provided, ct2stimer reloads systemd unit files (= `systemctl daemon-reload`) and
starts all generated timers (= `systemctl start foo.timer`). Maybe `sudo` is required to execute.

```bash
$ sudo ct2stimer -f sample.cron --reload
```

### Determine unit name from command to execute

As you know, crontab does not have the concept of "task name". However, task name is required to identify each systemd unit.
You can extract task name from original command using regular expression. `--name-regexp REGEXP` flag is used for this.
Regular expression must have one [capturing group](http://www.regular-expressions.info/brackets.html).

If regular expression is not provided or command does not match to the given regular expression, hash value,
which is calculated from command, is used for unit name.

```bash
$ ct2stimer -f sample.cron --name-regexp '--name ([a-zA-Z0-9_-]+)'
```

### Per-entry configuration (`# config:` comments)

Standard crontab has only minute resolution and no concept of a job name. Rather
than extend cron syntax, ct2stimer reads an optional `# config:` comment placed
above an entry. The configuration applies to the next schedule entry (blank lines
and ordinary comments in between are tolerated).

```cron
# config: TimerName=db-backup RunSecond=30
*/5 * * * * /usr/local/bin/backup
```

Supported keys:

| Key | Meaning |
|-----|---------|
| `TimerName` | Unit name for this entry. Takes precedence over `--name-regexp` and the hashed fallback. Allowed characters: letters, digits, `.`, `_`, `-`. |
| `RunSecond` | Second-of-the-minute (`0`–`59`) at which the timer fires. |

`RunSecond` exists to dodge collisions — e.g. a job reading a file at `:00` racing
a daemon that writes it at `:00`. The example above generates:

```ini
[Timer]
OnCalendar=*:0,5,10,15,20,25,30,35,40,45,50,55:30
AccuracySec=1s
```

`AccuracySec=1s` is set automatically whenever `RunSecond` is used: systemd
timers default to `AccuracySec=1min`, which would otherwise let the trigger drift
anywhere within the minute and defeat the offset.

Unrecognised keys (e.g. a typo) print a warning and are ignored; invalid values
(`RunSecond` out of range, an illegal `TimerName`) are reported with their line
number.

### Delete unregistered unit files

If `--delete` is provided, ct2timer deletes unit files which are no longer written in the given crontab file.

```bash
$ ct2stimer -f tmp/scheduler -o /run/systemd/system --delete
Deleted: /run/systemd/system/cron-19fb9c164fe8.service
Deleted: /run/systemd/system/cron-19fb9c164fe8.timer
Deleted: /run/systemd/system/cron-4f76a3902132.service
Deleted: /run/systemd/system/cron-4f76a3902132.timer
```

### Specify unit dependencies

You can specify unit dependencies (`After=`) with `--after AFTER` flag.

```bash
$ ct2stimer -f sample.crom --after docker.service
```

## Development

Requires Go (see the version in [`go.mod`](go.mod)). Dependencies are managed with
Go modules and downloaded automatically on first build.

```bash
$ git clone https://github.com/HQJaTu/ct2stimer
$ cd ct2stimer

$ make          # build bin/ct2stimer
$ make test     # go test -cover -race ./...
$ ./bin/ct2stimer -f sample.cron --dry-run
```

The systemd unit templates under `systemd/templates/` are embedded into the
binary via `//go:embed`, so there is no code-generation step.

## Author

Daisuke Fujita ([@dtan4](https://github.com/dtan4)),
Jari Turkia ([@HQJaTu](https://github.com/HQJaTu))

## License

[![MIT License](http://img.shields.io/badge/license-MIT-blue.svg?style=flat)](LICENSE)
