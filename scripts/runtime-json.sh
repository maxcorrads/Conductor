#!/bin/sh

runtime_root_version() {
  /usr/bin/plutil -extract version raw -o - "$1" 2>/dev/null
}
