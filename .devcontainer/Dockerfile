FROM debian:bullseye-slim

# Install BlueZ and D-Bus client libraries
# Note: We don't run bluetoothd in the container - we use the host's daemon via D-Bus
RUN apt-get update && apt-get install -y \
    bluez \
    bluetooth \
    libbluetooth-dev \
    libudev-dev \
    dbus \
    libdbus-1-dev \
    && rm -rf /var/lib/apt/lists/*

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

# Set working directory
WORKDIR /workspace

# The container will use the host's BlueZ daemon via mounted D-Bus socket
# No bluetoothd process should run inside this container