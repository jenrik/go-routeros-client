#!/bin/sh
set -e

export ROS_VERSION="${ROS_VERSION:-7.5}"
echo "RouterOS version: ${ROS_VERSION}"

docker run \
  --entrypoint "/routeros/entrypoint_with_four_interfaces.sh" \
  -p 443:443 \
  -p 2222:22 \
  -p 8728:8728 \
  -v /dev/net/tun:/dev/net/tun \
  --cap-add=NET_ADMIN \
  -t \
  -d \
  --rm \
  --name routeros \
  ghcr.io/jenrik/docker-routeros:${ROS_VERSION}

while ! nc -q0 127.0.0.1 8728 < /dev/null > /dev/null 2>&1; do
  echo "Failed to connect, waiting 10 seconds"
  sleep 10
done

echo "Waiting 30 seconds for remaining initialization"
sleep 30

export ROS_USERNAME=admin
export ROS_PASSWORD=
export ROS_IP_ADDRESS=127.0.0.1
