#!/bin/bash

set -e

GOARCH=arm go build -o odroid-helper-1.0.0/usr/bin/odroid-helper

chmod +x odroid-helper-1.0.0/usr/bin/odroid-helper

dpkg-deb --build odroid-helper-1.0.0
