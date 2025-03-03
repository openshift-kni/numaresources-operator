#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

CWD="$(realpath "$(dirname "$0")/..")"
pushd "${CWD}"

BOILERPLATE_FILENAME="hack/boilerplate.go.txt"
OUTPUT_FILE="_generated.defaults.go"
TARGET_DIR="pkg/objectstate/defaulter/generated"
API_PACKAGES=("./vendor/k8s.io/api/core/v1" "./vendor/k8s.io/api/apps/v1" "./vendor/k8s.io/api/rbac/v1")
KUBERNETES_VERSION=$(go list -m -f '{{.Replace.Version}}' k8s.io/kubernetes)

function download_codegen {
    local code_generator_version

    code_generator_version=$(go list -m -f '{{.Replace.Version}}' k8s.io/code-generator)
    go install k8s.io/code-generator/cmd/defaulter-gen@"${code_generator_version}"
}

function annotate_packages {
  local action=${1}  # Default action is "annotate"
  shift
  local api_packages=("$@")
  local sed_command

  if [[ "${action}" == "annotate" ]]; then
  echo "annotate packages"
  sed_command='/\/\/ +k8s:deepcopy-gen=package/a\/\/ +k8s:defaulter-gen=TypeMeta'
  fi

  if [[ "${action}" == "delete" ]]; then
    echo "delete annotation from packages"
    sed_command='/\/\/ +k8s:defaulter-gen=TypeMeta/d'
  fi

  for package in "${api_packages[@]}"
  do
    sed -i "${sed_command}" "${package}"/doc.go
  done
}

function api_group() {
    local package=${1}

    IFS='/' read -r -a tokens <<< "${package}"
    # token[4] = APIGroupName i.e. apps,core,rbac
    echo "${tokens[4]}"
}

function api_version() {
    local package=${1}

    IFS='/' read -r -a tokens <<< "${package}"
    # token[5] = APIVersion, i.e. v1,beta1,alpha1
    echo "${tokens[5]}"
}

function api_group_version() {
  local package=${1}
  local group
  local version

  group=$(api_group "${package}")
  version=$(api_version "${package}")
  echo "${group}${version}"
}

function pull_manual_defaults() {
    local api_packages=("$@")
    local manual_default_path
    local group_version
    local version

    for package in "${api_packages[@]}"
    do
      version=$(api_version "${package}")
      group_version=$(api_group_version "${package}")
      manual_default_path="https://raw.githubusercontent.com/kubernetes/kubernetes/refs/tags/${KUBERNETES_VERSION}/pkg/apis/$(api_group "${package}")/${version}/defaults.go"
      curl "${manual_default_path}" -o "${package}/defaults.go"

      # copy the defaults functions to the target directory because we'll need to refer to them later
      cp "${package}/defaults.go" "${TARGET_DIR}/${group_version}_defaults.go"

      # delete the line that imports the api module to avoid cyclic import
      sed -i "s#$group_version \"${package#./vendor/}\"\|$version \"${package#./vendor/}\"\$##" "${package}/defaults.go"

      # delete the group-version prefix from the code
      sed -i "s/${group_version}\.//" "${package}/defaults.go"
      sed -i "s/\*${group_version}\./*/" "${package}/defaults.go"

      # delete the version prefix from the code
      sed -i "s/${version}\.//" "${package}/defaults.go"
      sed -i "s/\*${version}\./*/" "${package}/defaults.go"

      if [[ "${package}" == "./vendor/k8s.io/api/core/v1" ]]; then
          # delete import path
          sed -i '/"k8s.io\/kubernetes\/pkg\/api\/v1\/service"/d' "${package}/defaults.go"

          # ugly hack to make the compiler happy
          sed -i 's/service.ExternallyAccessible/ExternallyAccessible/'  "${package}/defaults.go"
          cat <<EOF >> "${package}/defaults.go"
           func ExternallyAccessible(service *Service) bool {
            return service.Spec.Type == ServiceTypeLoadBalancer ||
              service.Spec.Type == ServiceTypeNodePort ||
              (service.Spec.Type == ServiceTypeClusterIP && len(service.Spec.ExternalIPs) > 0)
          }
EOF
      fi
    done
}

function format() {
  gofmt -s -w ${TARGET_DIR}
}

function cleanup() {
    annotate_packages "delete" "${API_PACKAGES[@]}"
    format
    go mod tidy
    go mod vendor
}

download_codegen

annotate_packages "annotate" "${API_PACKAGES[@]}"

pull_manual_defaults "${API_PACKAGES[@]}"

defaulter-gen \
  -v 4 \
  --go-header-file "${BOILERPLATE_FILENAME}" \
  --output-file "${OUTPUT_FILE}" \
  "${API_PACKAGES[@]}"

for package in "${API_PACKAGES[@]}"
  do
    APIGV=$(api_group_version "${package}")
    VERSION=$(api_version "${package}")
    GENERATED_FILE_NAME="${APIGV}""${OUTPUT_FILE}"

    # move generated to TARGET_DIR
    mv "${package}/${OUTPUT_FILE}" "${TARGET_DIR}"/"${GENERATED_FILE_NAME}"

    # change the package name to generated
    sed -i "s#package v1#package ${TARGET_DIR##*/}#" "${TARGET_DIR}"/"${GENERATED_FILE_NAME}"
    sed -i "s#package v1#package ${TARGET_DIR##*/}#" "${TARGET_DIR}/${APIGV}_defaults.go"

    # append import k8s.io/api/GROUP/VERSION to the generated files
    sed -i "/import (/a\    ${APIGV} \"${package#./vendor/}\"" "${TARGET_DIR}"/"${GENERATED_FILE_NAME}"

    # delete the RegisterDefaults function which is not needed for us
    sed -i '/^\/\/ RegisterDefaults/,/^}/d' "${TARGET_DIR}"/"${GENERATED_FILE_NAME}"

    # delete the add function which is not needed for us
    sed -i '/^func addDefaultingFuncs/,/^}/d' "${TARGET_DIR}/${APIGV}_defaults.go"

    # delete the line that imports runtime scheme import
    sed -i '/runtime/d' "${TARGET_DIR}"/"${GENERATED_FILE_NAME}"
    sed -i '/runtime/d' "${TARGET_DIR}/${APIGV}_defaults.go"

    # add the module prefix to some of the structs since we moved the files from there original package
    # so struct like DaemonSet or Pod should be refer as appsv1.DaemonSet and corev1.Pod respectively.
    sed -i "s/(in \*/(in *${APIGV}./" "${TARGET_DIR}"/"${GENERATED_FILE_NAME}"
    sed -i "s/ptrVar1 := Azure*/ptrVar1 := ${APIGV}.Azure/" "${TARGET_DIR}"/"${GENERATED_FILE_NAME}"
    sed -i "s/(Azure*/(${APIGV}.Azure/" "${TARGET_DIR}"/"${GENERATED_FILE_NAME}"
    sed -i 's/GroupName$/rbacv1.GroupName/' "${TARGET_DIR}/${APIGV}_defaults.go"

    # delete the version prefix from the SetDefaults* functions
    sed -i "s/\b${VERSION}\.SetDefaults/SetDefaults/" "${TARGET_DIR}"/"${GENERATED_FILE_NAME}"

done

cleanup
