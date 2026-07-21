#!/bin/sh
set -eu

if [ "$#" -ne 3 ]; then
  echo "usage: $0 BUILD_LOCK SOURCE_DIR LICENSE_DIR" >&2
  exit 2
fi

lock_path=$1
source_directory=$2
license_directory=$3

for command_name in jq curl shasum tar mktemp; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "required command not found: ${command_name}" >&2
    exit 1
  fi
done

if ! jq -e '
  .schemaVersion == 1 and
  (.components | type == "array" and length > 0) and
  all(.components[];
    (.name | type == "string" and length > 0) and
    (.source | type == "string" and length > 0) and
    (.sha256 | type == "string" and test("^[0-9a-f]{64}$")) and
    (.licenseFiles | type == "array" and length > 0 and all(.[]; type == "string" and length > 0))
  )
' "${lock_path}" >/dev/null; then
  echo "invalid Host Build Lock for license collection" >&2
  exit 1
fi

mkdir -p "${source_directory}" "${license_directory}"

download_path=
listing_path=
top_levels_path=
extraction_directory=
component_temporary=
cleanup() {
  [ -z "${download_path}" ] || rm -f "${download_path}"
  [ -z "${listing_path}" ] || rm -f "${listing_path}"
  [ -z "${top_levels_path}" ] || rm -f "${top_levels_path}"
  [ -z "${extraction_directory}" ] || rm -rf "${extraction_directory}"
  [ -z "${component_temporary}" ] || rm -rf "${component_temporary}"
}
trap cleanup EXIT HUP INT TERM

component_count=$(jq -r '.components | length' "${lock_path}")
component_index=0
while [ "${component_index}" -lt "${component_count}" ]; do
  component=$(jq -c ".components[${component_index}]" "${lock_path}")
  component_name=$(printf '%s\n' "${component}" | jq -r '.name')
  source_url=$(printf '%s\n' "${component}" | jq -r '.source')
  expected_sha256=$(printf '%s\n' "${component}" | jq -r '.sha256')

  case "${component_name}" in
    ""|[!a-z0-9]*|*[!a-z0-9._-]*)
      echo "invalid component name: ${component_name}" >&2
      exit 1
      ;;
  esac

  archive_path="${source_directory}/${component_name}.archive"
  if [ -e "${archive_path}" ]; then
    echo "source archive already exists: ${archive_path}" >&2
    exit 1
  fi
  download_path=$(mktemp "${source_directory}/.${component_name}.XXXXXX.tmp")

  curl --fail --location --silent --show-error --output "${download_path}" "${source_url}"
  actual_sha256=$(shasum -a 256 "${download_path}" | awk '{print $1}')
  if [ "${actual_sha256}" != "${expected_sha256}" ]; then
    echo "SHA-256 mismatch for ${component_name}" >&2
    exit 1
  fi
  mv "${download_path}" "${archive_path}"
  download_path=

  listing_path=$(mktemp "${source_directory}/.${component_name}.listing.XXXXXX.tmp")
  top_levels_path=$(mktemp "${source_directory}/.${component_name}.top-levels.XXXXXX.tmp")
  tar -tf "${archive_path}" >"${listing_path}"
  awk -F/ 'NF > 0 && $1 != "" {print $1}' "${listing_path}" | sort -u >"${top_levels_path}"
  top_level_count=$(awk 'NF {count++} END {print count + 0}' "${top_levels_path}")
  if [ "${top_level_count}" -ne 1 ]; then
    echo "source archive for ${component_name} must contain one top-level directory" >&2
    exit 1
  fi
  top_level=$(awk 'NF {print; exit}' "${top_levels_path}")
  if [ "${top_level}" = "." ] || [ "${top_level}" = ".." ]; then
    echo "source archive for ${component_name} has an invalid top-level directory" >&2
    exit 1
  fi

  component_output="${license_directory}/${component_name}"
  if [ -e "${component_output}" ]; then
    echo "license output already exists: ${component_output}" >&2
    exit 1
  fi
  extraction_directory=$(mktemp -d "${license_directory}/.${component_name}.extract.XXXXXX.tmp")
  component_temporary=$(mktemp -d "${license_directory}/.${component_name}.output.XXXXXX.tmp")

  license_count=$(printf '%s\n' "${component}" | jq -r '.licenseFiles | length')
  license_index=0
  while [ "${license_index}" -lt "${license_count}" ]; do
    license_path=$(printf '%s\n' "${component}" | jq -r ".licenseFiles[${license_index}]")
    case "${license_path}" in
      ""|.|..|/*|*\\*|../*|*/../*|*/..)
        echo "invalid license path for ${component_name}: ${license_path}" >&2
        exit 1
        ;;
    esac

    archive_entry="${top_level}/${license_path}"
    entry_count=$(awk -v expected="${archive_entry}" '$0 == expected {count++} END {print count + 0}' "${listing_path}")
    if [ "${entry_count}" -ne 1 ]; then
      echo "license ${license_path} for ${component_name} must occur exactly once in source archive" >&2
      exit 1
    fi

    tar -xf "${archive_path}" -C "${extraction_directory}" "${archive_entry}"
    extracted_path="${extraction_directory}/${archive_entry}"
    if [ ! -f "${extracted_path}" ] || [ -L "${extracted_path}" ]; then
      echo "license ${license_path} for ${component_name} is not a regular file" >&2
      exit 1
    fi

    destination_path="${component_temporary}/${license_path}"
    mkdir -p "$(dirname "${destination_path}")"
    cp "${extracted_path}" "${destination_path}"
    chmod 0644 "${destination_path}"

    license_index=$((license_index + 1))
  done

  mv "${component_temporary}" "${component_output}"
  component_temporary=
  rm -rf "${extraction_directory}"
  extraction_directory=
  rm -f "${listing_path}" "${top_levels_path}"
  listing_path=
  top_levels_path=

  component_index=$((component_index + 1))
done
