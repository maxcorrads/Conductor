#!/bin/sh

# Print -1, 0, or 1 when the first semantic version is older, equal to, or
# newer than the second. Build metadata is ignored as required by SemVer.
semver_compare() {
  awk -v left="$1" -v right="$2" '
    function valid_identifiers(value, rejectLeadingZero, identifiers, count, partIndex, numeric) {
      if (value == "") return 0
      count = split(value, identifiers, ".")
      for (partIndex = 1; partIndex <= count; partIndex++) {
        if (identifiers[partIndex] !~ /^[0-9A-Za-z-]+$/) return 0
        numeric = identifiers[partIndex] ~ /^[0-9]+$/
        if (rejectLeadingZero && numeric && length(identifiers[partIndex]) > 1 && substr(identifiers[partIndex], 1, 1) == "0") return 0
      }
      return 1
    }
    function prepare(value, numbers, side, plus, dash, metadata, count, partIndex) {
      plus = index(value, "+")
      if (plus > 0) {
        metadata = substr(value, plus + 1)
        if (!valid_identifiers(metadata, 0)) return 0
        value = substr(value, 1, plus - 1)
      }
      dash = index(value, "-")
      prerelease[side] = ""
      if (dash > 0) {
        prerelease[side] = substr(value, dash + 1)
        value = substr(value, 1, dash - 1)
        if (!valid_identifiers(prerelease[side], 1)) return 0
      }
      count = split(value, numbers, ".")
      if (count != 3) return 0
      for (partIndex = 1; partIndex <= 3; partIndex++) {
        if (numbers[partIndex] !~ /^[0-9]+$/) return 0
        if (length(numbers[partIndex]) > 1 && substr(numbers[partIndex], 1, 1) == "0") return 0
      }
      return 1
    }
    function compare_prerelease(leftValue, rightValue, leftParts, rightParts, leftCount, rightCount, partIndex, leftNumeric, rightNumeric) {
      if (leftValue == "" && rightValue == "") return 0
      if (leftValue == "") return 1
      if (rightValue == "") return -1
      leftCount = split(leftValue, leftParts, ".")
      rightCount = split(rightValue, rightParts, ".")
      for (partIndex = 1; partIndex <= leftCount && partIndex <= rightCount; partIndex++) {
        if (leftParts[partIndex] == "" || rightParts[partIndex] == "") return 2
        leftNumeric = leftParts[partIndex] ~ /^[0-9]+$/
        rightNumeric = rightParts[partIndex] ~ /^[0-9]+$/
        if (leftNumeric && length(leftParts[partIndex]) > 1 && substr(leftParts[partIndex], 1, 1) == "0") return 2
        if (rightNumeric && length(rightParts[partIndex]) > 1 && substr(rightParts[partIndex], 1, 1) == "0") return 2
        if (leftNumeric && rightNumeric) {
          if (leftParts[partIndex] + 0 < rightParts[partIndex] + 0) return -1
          if (leftParts[partIndex] + 0 > rightParts[partIndex] + 0) return 1
        } else if (leftNumeric != rightNumeric) {
          return leftNumeric ? -1 : 1
        } else {
          if (leftParts[partIndex] < rightParts[partIndex]) return -1
          if (leftParts[partIndex] > rightParts[partIndex]) return 1
        }
      }
      if (leftCount < rightCount) return -1
      if (leftCount > rightCount) return 1
      return 0
    }
    BEGIN {
      if (!prepare(left, leftNumbers, "left") || !prepare(right, rightNumbers, "right")) exit 2
      for (part = 1; part <= 3; part++) {
        if (leftNumbers[part] + 0 < rightNumbers[part] + 0) { print -1; exit }
        if (leftNumbers[part] + 0 > rightNumbers[part] + 0) { print 1; exit }
      }
      result = compare_prerelease(prerelease["left"], prerelease["right"])
      if (result == 2) exit 2
      print result
    }
  '
}

conductor_version_from_output() {
  case "$1" in
    "conductor "*) version=${1#conductor } ;;
    *) return 1 ;;
  esac
  semver_compare "$version" "$version" >/dev/null || return 1
  printf '%s\n' "$version"
}
