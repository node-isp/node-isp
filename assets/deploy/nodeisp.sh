#!/usr/bin/env bash
set -e
set -o noglob

# Node ISP - Radius Server Deployment Script=
# Usage:
#   curl https://raw.githubusercontent.com/node-isp/node-isp/main/assets/deploy/nodeisp.sh | sh -
#
# This script will install Docker, which is used to run Node ISP.
# It will also install the Node ISP server manager, and setup your instance.
#

RELEASE_URL="https://github.com/node-isp/node-isp/releases/latest/download"
DOWNLOADER=


# --- helper functions for logs ---
info(){
    echo '[INFO] ' "$@"
}
warn(){
    echo '[WARN] ' "$@" >&2
}
fatal(){
    echo '[ERROR] ' "$@" >&2
    exit 1
}

validate_environment(){
  info "Validating environment the systemd directory"
  NODE_ISP_SYSTEMD_DIR=${NODE_ISP_SYSTEMD_DIR:-/etc/systemd/system}

  # Ensure the systemd directory exists
  [ -d "$NODE_ISP_SYSTEMD_DIR" ] || fatal "Systemd directory does not exist"

  verify_downloader curl || verify_downloader wget || fatal 'Can not find curl or wget for downloading files'

  validate_arch
}

validate_arch(){
  # Detect architecture
  case $(uname -m) in
    amd64) ARCH=amd64 ;;
    x86_64) ARCH=amd64 ;;
    arm64) ARCH=arm64 ;;
    aarch64) ARCH=arm64 ;;
    *) fatal "Unsupported architecture" ;;
  esac

  info "Detected architecture: $ARCH"

  case $(uname -s) in
    Linux) OS=linux ;;
    *) fatal "Unsupported OS" ;;
  esac
}

install_docker(){
  # Install Docker, if not already installed

  if [ -x "$(command -v docker)" ]; then
    info "Docker is already installed, skipping"
    return
  fi

  info "Installing Docker"
  download /tmp/docker-install.sh https://get.docker.com
  sh /tmp/docker-install.sh
  rm /tmp/docker-install.sh
}

verify_downloader() {
    # Return failure if it doesn't exist or is no executable
    [ -x "$(command -v $1)" ] || return 1

    # Set verified executable as our downloader program and return success
    DOWNLOADER=$1
    return 0
}

download() {
    [ $# -eq 2 ] || fatal 'download needs exactly 2 arguments'
    set +e
    case $DOWNLOADER in
        curl)
              curl -o $1 -sfL $2
            ;;
        wget)
              wget -qO $1 $2
            ;;
        *)
            fatal "Incorrect executable '$DOWNLOADER'"
            ;;
    esac

    # Abort if download command failed
    [ $? -eq 0 ] || fatal 'Download failed'
    set -e
}

download_binary () {
  info "Downloading Node ISP - $RELEASE_URL/nodeisp_${OS}_${ARCH}"
  download /tmp/node-isp "$RELEASE_URL/nodeisp_${OS}_${ARCH}"

  # Install the binary
  install -m 755 /tmp/node-isp /usr/local/bin/node-isp

  # Cleanup
  rm /tmp/node-isp
}

create_systemd_service(){
  info "Creating systemd service"
  cat > "$NODE_ISP_SYSTEMD_DIR/node-isp.service" <<EOF
[Unit]
Description=Node ISP Server
After=docker.service
Requires=docker.service

[Service]
Type=simple
ExecStart=/usr/local/bin/node-isp server
Restart=always
RestartSec=5s

[Install]
WantedBy=multi-user.target
EOF

    # Reload systemd
    systemctl daemon-reload
}

run_nodeisp_setup() {
  # If a config file exists, we can skip the setup
  if [ -f /etc/node-isp/config.yaml ] ; then
    warn "Config file exists, skipping setup"
    return
  fi

  info "Running Node ISP setup"
  /usr/local/bin/node-isp setup

  # Test to ensure config was created @ /etc/node-isp/config.yaml
  [ -f /etc/node-isp/config.yaml ] || fatal "Node ISP setup failed"
}

print_success(){
  info "Node ISP Server has been successfully deployed"

  info "You can start the service by running: systemctl start node-isp"
  info "You can enable the service to start on boot by running: systemctl enable nnode-ispodeisp"
  info "You can check the status of the service by running: systemctl status node-isp"
  info "You can view the logs of the service by running: journalctl -u node-isp"
  info "Check the status of the Node ISP containers by running: docker ps"
}

{
  validate_environment
  install_docker
  download_binary
  run_nodeisp_setup
  create_systemd_service
  print_success
}