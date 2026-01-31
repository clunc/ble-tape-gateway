FROM debian:bullseye-slim

# Install BlueZ, D-Bus client libraries, git/ssh, and tools needed for Node/Codex
RUN apt-get update && apt-get install -y \
    bluez \
    bluetooth \
    libbluetooth-dev \
    libudev-dev \
    build-essential \
    libcap2-bin \
    dbus \
    libdbus-1-dev \
    curl \
    ca-certificates \
    sudo \
    git \
    openssh-client \
    && rm -rf /var/lib/apt/lists/*

# Install Node.js 20 (NodeSource) and the Claude/Codex CLIs
RUN curl -fsSL https://deb.nodesource.com/setup_20.x | bash - \
    && apt-get update \
    && apt-get install -y nodejs \
    && npm install -g @anthropic-ai/claude-code @openai/codex \
    && rm -rf /var/lib/apt/lists/*

# Install Go (official tarball)
ARG GO_VERSION=1.22.5
RUN curl -fsSL https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz -o /tmp/go.tgz \
    && rm -rf /usr/local/go \
    && tar -C /usr/local -xzf /tmp/go.tgz \
    && rm /tmp/go.tgz

# Go env for all users
ENV GOROOT=/usr/local/go
ENV GOPATH=/home/vscode/go
ENV PATH="${PATH}:/usr/local/go/bin:/home/vscode/go/bin"

# Build arguments for user/group configuration
# These should be passed from devcontainer.json to match host credentials
ARG USER_UID=1000
ARG USER_GID=1000
ARG BLUETOOTH_GID=113

# Create or modify bluetooth group to match host GID
# This is CRITICAL: BlueZ checks the numerical GID via SO_PEERCRED, not the group name
RUN groupadd -g ${BLUETOOTH_GID} bluetooth 2>/dev/null || \
    groupmod -g ${BLUETOOTH_GID} bluetooth 2>/dev/null || true

# Create vscode group with matching GID
RUN groupadd -g ${USER_GID} vscode 2>/dev/null || true

# Create vscode user with matching UID and add to bluetooth group
RUN useradd -u ${USER_UID} -g ${USER_GID} -G ${BLUETOOTH_GID} -m -s /bin/bash vscode

# Ensure the user is properly added to bluetooth group
RUN usermod -aG ${BLUETOOTH_GID} vscode

# Fix permissions on home directory
RUN chown -R ${USER_UID}:${USER_GID} /home/vscode

# Give vscode passwordless sudo (handy for dev, matches Life Cockpit style)
RUN echo "vscode ALL=(root) NOPASSWD:ALL" > /etc/sudoers.d/vscode \
    && chmod 0440 /etc/sudoers.d/vscode

# Create a D-Bus policy file for development
# This ensures the bluetooth group has proper permissions
RUN echo '<!DOCTYPE busconfig PUBLIC "-//freedesktop//DTD D-Bus Bus Configuration 1.0//EN" \
 "http://www.freedesktop.org/standards/dbus/1.0/busconfig.dtd"> \
<busconfig> \
  <policy group="bluetooth"> \
    <allow send_destination="org.bluez"/> \
    <allow send_interface="org.bluez.Agent1"/> \
    <allow send_interface="org.bluez.GattCharacteristic1"/> \
    <allow send_interface="org.bluez.GattDescriptor1"/> \
    <allow send_interface="org.bluez.LEAdvertisement1"/> \
    <allow send_interface="org.freedesktop.DBus.ObjectManager"/> \
    <allow send_interface="org.freedesktop.DBus.Properties"/> \
  </policy> \
</busconfig>' > /etc/dbus-1/system.d/bluetooth-devel.conf

# Switch to non-root user
USER vscode

# Install Delve for debugging
RUN /usr/local/go/bin/go install -v github.com/go-delve/delve/cmd/dlv@latest
RUN sudo setcap 'cap_net_raw,cap_net_admin+eip' /home/vscode/go/bin/dlv

# Set working directory
WORKDIR /workspace

# The container will use the host's BlueZ daemon via mounted D-Bus socket
# No bluetoothd process should run inside this container
