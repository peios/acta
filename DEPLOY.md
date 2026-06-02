# Deploying Acta

Operational runbook for a self-hosted, public Acta instance. The stack is
**Caddy** (TLS) → the **app** → **Postgres**, via Docker Compose. The app image
is pulled prebuilt from GHCR, so the box never compiles code and a **1 GB host
is plenty**.

Acta's origin is built to be safe reached directly. Putting a CDN such as
**Cloudflare** in front — and locking the origin to it — shrinks the attack
surface further. Both paths are covered: pick **§5A (standalone)** or **§5B
(behind Cloudflare)**.

Conventions: `<HOST_IP>` is the server, `acta.example.com` is your domain.
**Secrets are generated on the box and never pasted into a terminal you don't
control.**

## 0. Prerequisites

- [ ] A Linux host (Ubuntu 24.04+, **≥1 GB RAM**) with a public IP.
- [ ] Your SSH **public** key authorized for `root` (add it at the provider's
      create step, or append to `/root/.ssh/authorized_keys`).
- [ ] A domain with a DNS record at the host (see §5B for the CDN case).
- [ ] `ssh root@<HOST_IP>` works.

## 1. Harden the box

Run on the box. Standard hygiene for anything public-facing.

**Patch + automatic security updates (auto-reboot for kernel updates):**

```sh
export DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a
apt-get update && apt-get -y full-upgrade
apt-get -y install unattended-upgrades fail2ban
cat > /etc/apt/apt.conf.d/20auto-upgrades <<'EOF'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
EOF
cat > /etc/apt/apt.conf.d/52-autoreboot <<'EOF'
Unattended-Upgrade::Automatic-Reboot "true";
Unattended-Upgrade::Automatic-Reboot-Time "04:00";
EOF
systemctl enable --now unattended-upgrades fail2ban
```

**SSH — key only.** The filename matters: cloud images ship a
`50-cloud-init.conf` that *enables* password auth, and sshd uses the **first**
value it reads — so the override must sort before it (hence `00-`):

```sh
cat > /etc/ssh/sshd_config.d/00-hardening.conf <<'EOF'
PermitRootLogin prohibit-password
PasswordAuthentication no
KbdInteractiveAuthentication no
EOF
sshd -t && systemctl reload ssh
sshd -T | grep -E '^(passwordauthentication|permitrootlogin) '   # confirm
```

You're connected by key, so this won't lock you out — but keep the session open
until a fresh `ssh` succeeds.

**Swap cushion (1 GB hosts):**

```sh
fallocate -l 2G /swapfile && chmod 600 /swapfile && mkswap /swapfile && swapon /swapfile
echo '/swapfile none swap sw 0 0' >> /etc/fstab
echo 'vm.swappiness=10' > /etc/sysctl.d/99-swap.conf && sysctl --system >/dev/null
```

## 2. Install Docker (with log rotation)

Cap container logs *before* first run so they can't fill the disk:

```sh
mkdir -p /etc/docker
cat > /etc/docker/daemon.json <<'EOF'
{ "log-driver": "json-file", "log-opts": { "max-size": "30m", "max-file": "3" } }
EOF
curl -fsSL https://get.docker.com | sh
```

(If Docker was already installed, the caps apply to containers created from now
on — recreate existing ones later with `… up -d --force-recreate`.)

## 3. Fetch the deploy files

The box needs only the deploy bundle — compose file, Caddyfile, `.env` template.

```sh
mkdir -p /opt/acta && cd /opt/acta
curl -fsSL https://github.com/peios/acta/releases/latest/download/acta-deploy.tar.gz \
  | tar xz --strip-components=1
```

## 4. Configure secrets (.env)

Generated on the box; nothing is echoed:

```sh
cd /opt/acta
umask 077
{
  echo "ACTA_DOMAIN=acta.example.com"
  echo "ACTA_RP_NAME=Acta"
  echo "POSTGRES_PASSWORD=$(openssl rand -base64 32)"
  echo "ACTA_BOOTSTRAP_USERNAME=admin"
  echo "ACTA_BOOTSTRAP_PASSWORD=$(openssl rand -base64 24)"
} > .env
```

The bootstrap password is random and discarded — §7 sets a real one.

## 5. TLS — pick one

### A. Standalone (Let's Encrypt) — the default bundle

The bundled `Caddyfile` uses automatic HTTPS: Caddy fetches a Let's Encrypt
certificate on first request, which needs port 80 (or TLS-ALPN on 443)
reachable from the internet. Nothing to change — skip to §6. The origin is a
normal public HTTPS site, protected by the app's own hardening.

The rest of this guide assumes a compose alias; set it for **path A**:

```sh
alias dc='docker compose -f docker-compose.prod.yml'
```

### B. Behind Cloudflare (recommended if you use a CDN)

Cloudflare terminates public TLS; the origin presents a **Cloudflare Origin
Certificate** and is reachable *only* from Cloudflare (§8). This removes the
public attack surface and makes `CF-Connecting-IP` trustworthy (so the app sees
real client IPs).

In the Cloudflare dashboard:

- DNS record **proxied** (orange cloud).
- SSL/TLS mode **Full (strict)**.
- SSL/TLS → Origin Server → **Create Certificate**. Save the **certificate**
  and **private key** onto the box (paste them in your own SSH session — they
  must not pass through any other terminal):

```sh
mkdir -p /opt/acta/caddy/certs && chmod 700 /opt/acta/caddy/certs
cd /opt/acta/caddy/certs
cat > origin.pem   # paste the certificate, Enter, Ctrl-D
cat > origin.key   # paste the private key, Enter, Ctrl-D
chmod 600 origin.pem origin.key
```

Replace the `Caddyfile` to serve that cert. Since the origin is CDN-only,
*trust* `CF-Connecting-IP` (the default bundle strips it, which is correct only
when the origin is directly reachable):

```sh
cat > /opt/acta/Caddyfile <<'EOF'
{$ACTA_DOMAIN} {
	tls /etc/caddy/certs/origin.pem /etc/caddy/certs/origin.key
	encode zstd gzip
	reverse_proxy app:8080
}
EOF
```

Add a compose overlay to mount the cert into Caddy, and set the alias to include
it for **path B**:

```sh
cat > /opt/acta/docker-compose.cloudflare.yml <<'EOF'
services:
  caddy:
    volumes:
      - ./caddy/certs:/etc/caddy/certs:ro
EOF
alias dc='docker compose -f docker-compose.prod.yml -f docker-compose.cloudflare.yml'
```

## 6. Launch + verify

```sh
cd /opt/acta
dc up -d
dc ps                  # all healthy
```

Local origin check (use `-k` on path B — the origin cert is CDN-only, not
publicly trusted):

```sh
curl -sk --resolve acta.example.com:443:127.0.0.1 https://acta.example.com/healthz   # -> ok
curl -sk --resolve acta.example.com:443:127.0.0.1 https://acta.example.com/readyz    # -> ok (DB)
```

Then through the public hostname:

```sh
curl -fsS https://acta.example.com/healthz   # -> ok, with a valid public cert
```

## 7. Set the admin password

The interactive prompt keeps the password out of argv and shell history:

```sh
docker exec -it acta-app-1 /acta-server setpassword -username admin
```

Sign in at `https://acta.example.com`. (The same command is your lockout
recovery; it also revokes existing sessions.)

## 8. Firewall — and the Docker/ufw gotcha

Base host firewall — **allow SSH before enabling**, or you lock yourself out:

```sh
ufw default deny incoming
ufw default allow outgoing
ufw allow 22/tcp
ufw allow 443/tcp           # but read the caveat below
ufw --force enable
```

> **Docker bypasses ufw.** Docker publishes container ports by inserting its own
> rules in the iptables `FORWARD`/`DOCKER` chains, which skip ufw's `INPUT`
> chain. A ufw rule restricting `:443` therefore has **no effect** on a
> Docker-published port — it stays open to the world. ufw still governs SSH and
> any host-level (non-Docker) service.

On **path A** that's fine — `:443` is meant to be public. On **path B** you must
filter the Docker-published port via the **`DOCKER-USER`** chain, the hook
Docker provides for exactly this (evaluated before its own accept rules). Save
your CDN's published ranges and install an idempotent gate (Cloudflare shown):

```sh
mkdir -p /etc/acta
curl -fsS https://www.cloudflare.com/ips-v4 -o /etc/acta/cf-ips-v4
curl -fsS https://www.cloudflare.com/ips-v6 -o /etc/acta/cf-ips-v6

cat > /usr/local/sbin/acta-cf-fw.sh <<'EOF'
#!/usr/bin/env bash
# Gate the Docker-published :443 to a CDN's ranges via DOCKER-USER. Idempotent.
ensure() {
  local ipt=$1 chain=$2 file=$3
  $ipt -N DOCKER-USER 2>/dev/null
  $ipt -N "$chain" 2>/dev/null || $ipt -F "$chain"
  $ipt -C DOCKER-USER -j "$chain" 2>/dev/null || $ipt -I DOCKER-USER -j "$chain"
  while read -r r || [ -n "$r" ]; do
    [ -n "$r" ] && $ipt -A "$chain" -p tcp --dport 443 -s "$r" -j RETURN
  done < "$file"
  $ipt -A "$chain" -p tcp --dport 443 -j DROP
}
ensure iptables  ACTA-CF  /etc/acta/cf-ips-v4
ensure ip6tables ACTA-CF6 /etc/acta/cf-ips-v6 2>/dev/null || true
EOF
chmod +x /usr/local/sbin/acta-cf-fw.sh
```

iptables rules don't survive a reboot, so re-apply on boot, after Docker
recreates its chains:

```sh
cat > /etc/systemd/system/acta-cf-fw.service <<'EOF'
[Unit]
Description=Gate Docker-published :443 to the CDN
After=docker.service
Requires=docker.service
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/local/sbin/acta-cf-fw.sh
[Install]
WantedBy=multi-user.target
EOF
systemctl enable --now acta-cf-fw.service
```

Verify the gate — through the CDN works, direct to the origin IP is refused:

```sh
curl -fsS https://acta.example.com/healthz                                              # ok
curl -sk --max-time 8 --resolve acta.example.com:443:<HOST_IP> https://acta.example.com/healthz   # should time out
```

> Re-run `/usr/local/sbin/acta-cf-fw.sh` after anything that churns Docker's
> iptables (daemon restart, `--force-recreate`); it's idempotent.

## 9. Hands-off maintenance

**Nightly Postgres backup** (14-day retention, atomic write, journald-logged):

```sh
cat > /usr/local/sbin/acta-backup.sh <<'EOF'
#!/usr/bin/env bash
set -uo pipefail
DIR=/var/backups/acta; mkdir -p "$DIR"
out="$DIR/acta-$(date +%F).sql.gz"; tmp="$out.tmp"
if docker exec acta-db-1 pg_dump -U acta acta | gzip > "$tmp"; then
  mv -f "$tmp" "$out"
  find "$DIR" -name 'acta-*.sql.gz' -mtime +14 -delete
  logger -t acta-backup "ok: $out"
else
  rm -f "$tmp"; logger -t acta-backup "FAILED"; exit 1
fi
EOF
chmod +x /usr/local/sbin/acta-backup.sh

cat > /etc/systemd/system/acta-backup.service <<'EOF'
[Unit]
Description=Acta Postgres backup
After=docker.service
Requires=docker.service
[Service]
Type=oneshot
ExecStart=/usr/local/sbin/acta-backup.sh
EOF
cat > /etc/systemd/system/acta-backup.timer <<'EOF'
[Unit]
Description=Nightly Acta Postgres backup
[Timer]
OnCalendar=*-*-* 03:30:00
Persistent=true
RandomizedDelaySec=300
[Install]
WantedBy=timers.target
EOF
systemctl enable --now acta-backup.timer
```

Restore: `gunzip -c <dump> | docker exec -i acta-db-1 psql -U acta acta`.

> Backups land on the same box. For real disaster-proofing, copy them off-site
> too — e.g. an `rclone` job to object storage.

**CDN IP-range refresh** — *path B only*. The allowlist is a snapshot; CDNs
change ranges occasionally, so refresh weekly and re-apply:

```sh
cat > /usr/local/sbin/acta-cf-fw-refresh.sh <<'EOF'
#!/usr/bin/env bash
set -uo pipefail
for v in v4 v6; do
  if curl -fsS "https://www.cloudflare.com/ips-$v" -o "/etc/acta/cf-ips-$v.new" \
     && [ -s "/etc/acta/cf-ips-$v.new" ]; then
    mv -f "/etc/acta/cf-ips-$v.new" "/etc/acta/cf-ips-$v"
  else rm -f "/etc/acta/cf-ips-$v.new"; fi
done
/usr/local/sbin/acta-cf-fw.sh
EOF
chmod +x /usr/local/sbin/acta-cf-fw-refresh.sh

cat > /etc/systemd/system/acta-cf-fw-refresh.service <<'EOF'
[Unit]
Description=Refresh CDN IP ranges and reapply origin gate
After=docker.service network-online.target
[Service]
Type=oneshot
ExecStart=/usr/local/sbin/acta-cf-fw-refresh.sh
EOF
cat > /etc/systemd/system/acta-cf-fw-refresh.timer <<'EOF'
[Unit]
Description=Weekly CDN IP-range refresh
[Timer]
OnCalendar=Mon *-*-* 04:30:00
Persistent=true
RandomizedDelaySec=600
[Install]
WantedBy=timers.target
EOF
systemctl enable --now acta-cf-fw-refresh.timer
```

**Monthly image prune** — clears old images left behind by updates:

```sh
cat > /etc/systemd/system/acta-image-prune.service <<'EOF'
[Unit]
Description=Prune dangling Docker images
Requires=docker.service
After=docker.service
[Service]
Type=oneshot
ExecStart=/usr/bin/docker image prune -f
EOF
cat > /etc/systemd/system/acta-image-prune.timer <<'EOF'
[Unit]
Description=Monthly Docker image prune
[Timer]
OnCalendar=*-*-01 05:00:00
Persistent=true
[Install]
WantedBy=timers.target
EOF
systemctl enable --now acta-image-prune.timer
```

With these in place the box self-maintains: daily patching (04:00 auto-reboot
for kernels), nightly backups, weekly CDN-range refresh, monthly prune, capped
logs, and containers that restart on crash or reboot. TLS is hands-off —
Cloudflare's edge cert auto-renews and the Origin cert lasts ~15 years (path B),
or Caddy auto-renews Let's Encrypt (path A).

## 10. Monitoring

Run an **external** uptime check — the one thing the box can't do for itself, as
it can't report its own death. Point any monitor (UptimeRobot, Better Stack,
Healthchecks.io, Cloudflare Health Checks) at:

- `https://acta.example.com/healthz` — liveness (expect `200` + body `ok`)
- optionally `…/readyz` — also verifies the database

## Updating

Pull the newest image and recreate (pin `ACTA_VERSION` in `.env` for
reproducible deploys):

```sh
cd /opt/acta
dc pull && dc up -d
```

If the compose file or Caddyfile changed upstream, re-fetch the bundle (§3) —
it won't touch your `.env` or certs. After a `--force-recreate`, re-run the
firewall script (§8) on path B.

## Disaster recovery

State and host-specific config to capture so you can rebuild on a fresh box:

- `/opt/acta/.env` — secrets. **Not** in the bundle; back it up securely.
- `/opt/acta/caddy/certs/` — origin cert + key (path B).
- `/var/backups/acta/` — the Postgres dumps.
- This runbook — §1, §8 and §9 are re-runnable as-is.

To restore: provision a new box, run §1–§5, restore `.env` and the certs,
`dc up -d`, then load the latest dump:

```sh
gunzip -c /var/backups/acta/acta-YYYY-MM-DD.sql.gz | docker exec -i acta-db-1 psql -U acta acta
```
