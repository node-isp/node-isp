#!/usr/bin/env bash
set -e
set -o noglob

# Node ISP - Radius Server Deployment Script=
# Usage:
#   curl https://raw.githubusercontent.com/node-isp/node-isp/main/assets/deploy/radius.sh | ENV_VAR=... sh -
#
# Environment Variables:
#   NODE_ISP_DOMAIN (required): The base domain or subdomain of your Node ISP instance, eg portal.myisp.au.
#   NODE_ISP_RADIUS_TOKEN (required): The shared secret for the RADIUS server.
#   NODE_ISP_RADIUS_CACHE_DIR (optional): The directory to store the RADIUS cache files, defaults to /var/lib/nodeisp/radius.
#   NODE_ISP_SYSTEMD_DIR (optional): The directory to store the systemd service files, defaults to /etc/systemd/system.
#
# Example:
# curl ... | NODE_ISP_DOMAIN=portal.myisp.au NODE_ISP_RADIUS_TOKEN=secret sh -
#
# This script will install Docker, which is used to run FreeRADIUS.
# It will also install the Node ISP FreeRADIUS manager, which manages the RADIUS server configuration.
#
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
  info "Validating environment"

  # Check if the required environment variables are set
  if [ -z "$NODE_ISP_DOMAIN" ]; then
    fatal "NODE_ISP_DOMAIN is required"
  fi

  if [ -z "$NODE_ISP_RADIUS_TOKEN" ]; then
    fatal "NODE_ISP_RADIUS_TOKEN is required"
  fi

  # Set the cache directory
  NODE_ISP_RADIUS_CACHE_DIR=${NODE_ISP_RADIUS_CACHE_DIR:-/var/lib/nodeisp/radius}

  # Set the systemd directory
  NODE_ISP_SYSTEMD_DIR=${NODE_ISP_SYSTEMD_DIR:-/etc/systemd/system}

  # Create the cache directory, if it doesn't exist, exit if it fails
  mkdir -p "$NODE_ISP_RADIUS_CACHE_DIR" || fatal "Failed to create cache directory"

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
  info "Downloading binary - $RELEASE_URL/radius_${OS}_${ARCH}"
  download /tmp/radius "$RELEASE_URL/radius_${OS}_${ARCH}"

  # Install the binary
  install -m 755 /tmp/radius /usr/local/bin/radius
}

create_systemd_service(){
  info "Creating systemd service"
  cat > "$NODE_ISP_SYSTEMD_DIR/nodeisp_radius.service" <<EOF
[Unit]
Description=Node ISP RADIUS Server
After=docker.service
Requires=docker.service

[Service]
Type=simple
Environment="NODE_ISP_DOMAIN=$NODE_ISP_DOMAIN"
Environment="NODE_ISP_RADIUS_TOKEN=$NODE_ISP_RADIUS_TOKEN"
Environment="NODE_ISP_RADIUS_CACHE_DIR=$NODE_ISP_RADIUS_CACHE_DIR"
ExecStart=/usr/local/bin/radius
Restart=always
RestartSec=5s

[Install]
WantedBy=multi-user.target
EOF

    # Reload systemd
    systemctl daemon-reload
}

print_success(){
  info "Node ISP RADIUS Server has been successfully deployed"

  info "You can start the service by running: systemctl start nodeisp_radius"
  info "You can enable the service to start on boot by running: systemctl enable nodeisp_radius"
  info "You can check the status of the service by running: systemctl status nodeisp_radius"
  info "You can view the logs of the service by running: journalctl -u nodeisp_radius"
  info "Check the status of the FreeRADIUS container by running: docker ps"
}

{
  validate_environment
  install_docker
  download_binary
  create_systemd_service
  print_success
}