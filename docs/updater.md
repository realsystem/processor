# Updating the control plane from GitHub through Telegram

A small Telegram bot lets the owner update the processor control plane with `/update <code>`. It
deploys **release tags only**, requires **green CI** on the tagged commit, checks health afterwards and
**rolls back automatically**. It is optional and runs next to a control plane started from
`docker-compose.deploy.yml`. It updates the control plane only; a worker is updated where it runs
(for Windows, re-run `scripts/install-windows.ps1`).

If the control plane is installed by `edge_server`, that repository pins the image and has its own
`/redeploy`; use one mechanism per host, not both.

## How it works

```
Telegram  <-- long poll (outbound only) --  updater-bot (container, no privileges)
                                               | writes requests/update.request (tag + request id)
                                               v
                       processor-update.path  -->  processor-update.service (root, one shot)
                                               | re-validates the request, docker compose pull/up,
                                               | health check, rollback on failure
                                               v
                           results/update-result.json  --> bot messages you the outcome
```

1. `/update 123456` is accepted only from an allowlisted user, in a private chat, from a message younger than
   2 minutes, with exactly six digits.
2. The code must be the owner's current authenticator code (one step of clock skew is tolerated). Each
   30-second code works **once**: the last accepted step is stored on disk, so a restart does not forget it.
   Five wrong or reused codes in 15 minutes lock the command for 15 minutes (also persisted). Codes are never
   logged; the bot deletes the message that carried the code.
3. The bot asks the GitHub API for the newest tag matching `vMAJOR.MINOR[.PATCH]` (nothing else counts, so a
   merge to `main` reaches nothing until a tag is cut), then for the check runs of that tag's commit.
4. The request is handed over through a directory; the root-owned script does the work and writes a result
   file. A second request while one runs is refused.

The tag list is read with a conditional request (ETag), so repeated attempts do not use up the rate limit.

## Privileges

- **Bot container:** user 10001, read-only root file system, all capabilities dropped, `no-new-privileges`,
  no published ports, **no Docker socket**. It can write two directories (`requests/` and `bot/`), read one
  (`results/`) and reach `api.telegram.org` and `api.github.com`. That is the minimum for what it does: it
  cannot start, stop or change anything on the host by itself.
- **Update unit:** runs as root because `docker compose` needs it. It is a fixed script owned by root that
  only accepts a tag matching the release pattern and a hex request id; it never executes request content,
  never follows a symlink request, and writes results into a directory the bot cannot write.
- What a compromised bot could still do: ask for an update to the newest release tag that has green CI, which
  is also what you can do. It cannot choose a branch, a commit or an image.

## Enabling it

On the host that runs the control plane (`/opt/processor` with `docker-compose.deploy.yml` and `.env`):

1. **Create the bot** with @BotFather in Telegram and keep the token.
2. **Install the host side** (from a checkout of this repository): `sudo scripts/install-updater.sh`.
   Set `PROCESSOR_DIR` in `/etc/processor-updater/update.env` if the control plane is not in `/opt/processor`.
3. **Token:** save it as the only content of `/etc/processor-updater/telegram_token`, then
   `sudo chown 10001:10001 /etc/processor-updater/telegram_token && sudo chmod 0400 /etc/processor-updater/telegram_token`.
4. **Settings:** edit `/etc/processor-updater/updater.env` (template: `deploy/updater.env.example`):
   `UPDATER_REPO` (`owner/name`) and, after step 6, `UPDATER_ALLOWED_IDS`. `UPDATER_REQUIRE_CI` defaults to
   `true`; setting it to `false` skips the CI check and logs a warning.
5. **Start the bot:** `sudo docker compose -f docker-compose.updater.yml up -d --build`.
6. **Find your id:** send `/id` to the bot, put the number in `UPDATER_ALLOWED_IDS`, restart the bot
   (`sudo docker compose -f docker-compose.updater.yml restart`). Everyone else is ignored.
7. **TOTP:** `sudo docker compose -f docker-compose.updater.yml run --rm updater-bot --totp-setup`. It prints the
   secret and an `otpauth://` URI **once**; add it to an authenticator app. The secret is stored only in
   `/var/lib/processor-updater/bot/totp.secret` (mode 600). The command refuses to overwrite it (`--force`
   replaces it).
8. Try `/status`, then `/update <code>` once a tag exists.

Commands: `/help`, `/id`, `/status` (version, uptime, last update result), `/update <code>`.

A GHCR package that is private needs `docker login ghcr.io` on the host (as root) with a token that can read packages.

## Cutting a release

The bot never deploys anything that is not a tag. To release:

1. Make sure `main` has green CI.
2. Tag and push (this session cannot push tags; do it yourself):
   `git tag -a v1.0.0 -m "Release 1.0.0" && git push origin v1.0.0`
3. The `Images` workflow publishes `ghcr.io/<owner>/processor-control-plane:v1.0.0`. Its run is a check run on the
   tagged commit, so `/update` says "CI is still running" until the image exists.
4. `/update <code>`.

## What is refused (and what you are told)

| Situation | Reply |
|---|---|
| user not on the allowlist | no reply (the attempt is audited) |
| group chat | "only accepted in a private chat" |
| message older than 2 minutes, not 6 digits, extra arguments | "too old" / usage |
| wrong code / code already used | "Wrong code" / "already used"; five in 15 minutes lock it |
| locked out | remaining lock time, even for a right code |
| update in progress | "already in progress" (before a code is spent) |
| no `vX.Y[.Z]` tag | "Cut a tag first" |
| GitHub rate limit or error | rate-limit reset time / "try again later"; nothing is deployed |
| already on the newest tag | "Already on the newest release" |
| deployed version newer than the newest tag | "Not downgrading" |
| CI pending / failed / none / unreadable | one distinct message each; nothing is deployed |

A refusal does not use up the code; only an accepted request does. Every attempt (user id, command, outcome,
never the code) is appended to `/var/lib/processor-updater/bot/audit.log`.

## Outcomes

The bot messages you when the result file appears, even if the bot restarted meanwhile (it keeps a marker):
`success`, `rolled_back` (health check failed, previous version restored), `failed` (for example the image could
not be pulled, or the rollback was unhealthy too) or `refused` (by the host script).

## Recovering by hand

```sh
sudo journalctl -u processor-update.service -n 100          # what the script did
cat /var/lib/processor-updater/results/update-result.json   # last result
grep PROCESSOR_TAG /opt/processor/.env                      # the version compose uses
# pin a version yourself:
cd /opt/processor && sudo sed -i 's/^PROCESSOR_TAG=.*/PROCESSOR_TAG=v1.0.0/' .env \
  && sudo docker compose -f docker-compose.deploy.yml up -d
# a stuck request (host unit not running): sudo systemctl status processor-update.path; remove
# /var/lib/processor-updater/requests/update.request and /var/lib/processor-updater/bot/notify.json
```

Turn the feature off with `sudo systemctl disable --now processor-update.path` and
`sudo docker compose -f docker-compose.updater.yml down`.

## Limits

- Only the newest 500 tags are examined (five pages of 100).
- The health check is `HEALTH_URL` (default `http://127.0.0.1:8080/api/v1/health`) for `HEALTH_TIMEOUT` seconds
  (default 120); set both in `/etc/processor-updater/update.env` if the control plane listens elsewhere.
- Not covered: the worker, database migrations that cannot be undone by running the old image (the control
  plane runs migrations on start; back up `processor-data` before a major release), and updating the bot itself
  (rebuild it from a newer checkout; the pending-result marker survives the restart).
