#!/usr/bin/execlineb -P

cloud-hypervisor --api-socket /var/run/virtink/ch.sock --event-monitor path=/var/run/virtink/ch-events.json
