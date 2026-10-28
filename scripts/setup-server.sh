#!/usr/bin/env bash
# One-time setup of a fresh Ubuntu server, run as root:
#   ssh root@SERVER 'bash -s' < scripts/setup-server.sh
# Creates the deploy user with root's SSH keys, allows only key logins,
# opens only SSH, HTTP and HTTPS, installs Docker and adds 1 GB of swap.
# Safe to run again.
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get -y -qq -o Dpkg::Options::=--force-confold upgrade

if ! id deploy >/dev/null 2>&1; then
	adduser --disabled-password --gecos "" deploy
fi
usermod -aG sudo deploy
echo "deploy ALL=(ALL) NOPASSWD:ALL" >/etc/sudoers.d/90-deploy
chmod 440 /etc/sudoers.d/90-deploy
visudo -cf /etc/sudoers.d/90-deploy
install -d -m 700 -o deploy -g deploy /home/deploy/.ssh
install -m 600 -o deploy -g deploy /root/.ssh/authorized_keys /home/deploy/.ssh/authorized_keys

# The first value wins in sshd, so this file must sort before cloud-init's.
cat >/etc/ssh/sshd_config.d/00-hardening.conf <<'EOF'
PermitRootLogin no
PasswordAuthentication no
KbdInteractiveAuthentication no
EOF
sshd -t
systemctl reload ssh

ufw default deny incoming
ufw default allow outgoing
ufw allow 22/tcp comment ssh
ufw allow 80/tcp comment http
ufw allow 443/tcp comment https
ufw allow 443/udp comment http3
ufw --force enable

if ! command -v docker >/dev/null; then
	install -m 0755 -d /etc/apt/keyrings
	curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
	chmod a+r /etc/apt/keyrings/docker.asc
	. /etc/os-release
	echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $VERSION_CODENAME stable" \
		>/etc/apt/sources.list.d/docker.list
	apt-get update -qq
	apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
fi
usermod -aG docker deploy

if [ ! -f /swapfile ]; then
	fallocate -l 1G /swapfile
	chmod 600 /swapfile
	mkswap /swapfile
	swapon /swapfile
	echo "/swapfile none swap sw 0 0" >>/etc/fstab
fi

install -d -m 755 -o deploy -g deploy /opt/poker /opt/caddy-sites
echo "setup done; log in as deploy from now on"
[ -f /var/run/reboot-required ] && echo "reboot required"
