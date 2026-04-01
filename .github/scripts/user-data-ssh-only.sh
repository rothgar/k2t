#!/bin/bash
# cloud-init user-data: set up SSH key only.
# Used for worker nodes that will be configured via SSH after launch.
set -euo pipefail

mkdir -p /home/ubuntu/.ssh
echo "__SSH_PUBLIC_KEY__" >> /home/ubuntu/.ssh/authorized_keys
chmod 700 /home/ubuntu/.ssh
chmod 600 /home/ubuntu/.ssh/authorized_keys
chown -R ubuntu:ubuntu /home/ubuntu/.ssh
