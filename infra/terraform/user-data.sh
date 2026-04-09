#!/bin/bash
set -euo pipefail

# --- Docker ---
apt-get update -y
apt-get install -y ca-certificates curl gnupg awscli snapd

install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
chmod a+r /etc/apt/keyrings/docker.gpg

echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] \
  https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable" \
  > /etc/apt/sources.list.d/docker.list

apt-get update -y
apt-get install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin

systemctl enable docker
systemctl start docker
usermod -aG docker ubuntu

# --- AWS SSM Agent ---
snap install amazon-ssm-agent --classic
systemctl enable snap.amazon-ssm-agent.amazon-ssm-agent.service
systemctl restart snap.amazon-ssm-agent.amazon-ssm-agent.service

# --- JucoBot directory structure ---
mkdir -p /opt/${project_name}/{releases,shared}
chown -R ubuntu:ubuntu /opt/${project_name}

# --- Docker network ---
docker network create ${project_name}-stack 2>/dev/null || true

# --- Tailscale ---
curl -fsSL https://tailscale.com/install.sh | sh
tailscale up --authkey="${tailscale_auth_key}" --hostname="${project_name}-aws"

echo "=== user-data complete ==="
