<p align="center"><a href="https://theitdept.au" target="_blank"><img src="https://raw.githubusercontent.com/node-isp/node-isp/main/assets/logo.svg" width="400" alt="Node ISP Logo"></a></p>

## Introduction

Node ISP is a collection of services and tools to help you build a modern Internet Service Provider. The project is
designed to be modular, and you can choose to deploy only the services you need.

## Bugs

Please report any bugs or feature requests to
the [issue tracker](https://github.com/node-isp/node-isp/issues/new/choose).

## Feature Requests

Please start a new discussion in the [discussions](https://github.com/node-isp/node-isp/discussions)
section.

## Usage

Notes: To deploy the project, run the following command on a freshly provisioned Debian 12 or Ubuntu 22.04/24.04 server
with Docker installed.

1. Deploy a new server, running either Debian 12 or Ubuntu 22.04
2. Follow the docker installation instructions for your distribution (https://docs.docker.com/engine/install/debian/
   or https://docs.docker.com/engine/install/ubuntu/)
3. Install git
4. Clone this repository
5. Run the deployment script

```bash
apt update && apt upgrade -y

sudo apt install -y git software-properties-common curl apt-transport-https ca-certificates

# Add Docker GPG key to apt (Debian 12 and Ubuntu 22.04/24.04)
sudo install -m 0755 -d /etc/apt/keyrings
sudo curl -fsSL https://download.docker.com/linux/debian/gpg -o /etc/apt/keyrings/docker.asc
sudo chmod a+r /etc/apt/keyrings/docker.asc

# Debian12 
echo \
  "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/debian \
  $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | \
  sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
sudo apt update


# Ubuntu 22.04 or 24.04
echo \
  "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu \
  $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | \
  sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
sudo apt-get update

# Install docker and docker-compose
sudo apt install docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

# Clone the NodeISP deployment repository
cd /opt/
git clone https://github.com/node-isp/node-isp.git nodeisp


# Run the configuration script, answer the questions and wait for the script to finish
cd nodeisp
./setup.sh

# Edit the .env file and configure email settings, and other services

# Generate a Google Maps key here: https://developers.google.com/maps/gmp-get-started, tied to your domain.
vim .env

# Start the services
docker compose up -d

# Check the status of the services, and wait till the nodeisp-app service is healthy.
docker compose ps
```
