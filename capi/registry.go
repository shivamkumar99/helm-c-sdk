package main

/*
#include <stdint.h>
*/
import "C"

import (
	"github.com/shivamkumar99/helm-c-sdk/internal/handles"
	"github.com/shivamkumar99/helm-c-sdk/pkg/cerrors"
	"github.com/shivamkumar99/helm-c-sdk/pkg/wrapper"
)

// helm_registry_client_new creates an OCI registry client (opts_json
// optional, may be NULL). *out receives a handle; free with
// helm_registry_client_free.
//
//export helm_registry_client_new
func helm_registry_client_new(optsJSON *C.char, out *C.uint64_t, errOut **C.char) (code C.int32_t) {
	clearErrorOut(errOut)
	defer recoverToCode(&code, errOut)
	if out == nil {
		return failure(errOut, cerrors.New(cerrors.CodeInvalidArg, "out must not be NULL"))
	}
	opts, err := wrapper.ParseRegistryClientOptions(optionalGoString(optsJSON))
	if err != nil {
		return failure(errOut, err)
	}
	client, err := wrapper.NewRegistryClient(opts)
	if err != nil {
		return failure(errOut, err)
	}
	*out = C.uint64_t(registry.Put(handles.TypeRegistryClient, client))
	return C.int32_t(cerrors.CodeOK)
}

// helm_registry_client_free releases a registry-client handle. Type-checked
// and idempotent like every handle free.
//
//export helm_registry_client_free
func helm_registry_client_free(h C.uint64_t, errOut **C.char) C.int32_t {
	return freeTyped(h, handles.TypeRegistryClient, errOut, nil)
}

// helm_registry_login authenticates the client against host with basic
// credentials (opts_json optional: insecure, plain_http, cert_file,
// key_file, ca_file). The password is used for the login call only — never
// logged or echoed.
//
//export helm_registry_login
func helm_registry_login(h C.uint64_t, host, username, password, optsJSON *C.char, errOut **C.char) C.int32_t {
	return statusResult(errOut, func() error {
		if err := requireArgs("host, username and password", host, username, password); err != nil {
			return err
		}
		obj, err := registry.Get(uint64(h), handles.TypeRegistryClient)
		if err != nil {
			return err
		}
		opts, err := wrapper.ParseLoginOptions(optionalGoString(optsJSON))
		if err != nil {
			return err
		}
		return wrapper.RegistryLogin(obj, C.GoString(host), C.GoString(username), C.GoString(password), opts)
	})
}

// helm_registry_logout removes the client's stored credentials for host.
//
//export helm_registry_logout
func helm_registry_logout(h C.uint64_t, host *C.char, errOut **C.char) C.int32_t {
	return statusResult(errOut, func() error {
		if err := requireArgs("host", host); err != nil {
			return err
		}
		obj, err := registry.Get(uint64(h), handles.TypeRegistryClient)
		if err != nil {
			return err
		}
		return wrapper.RegistryLogout(obj, C.GoString(host))
	})
}

// registryClientOrNil resolves client_h, where 0 means "no client".
func registryClientOrNil(h C.uint64_t) (any, error) {
	if h == 0 {
		return nil, nil
	}
	return registry.Get(uint64(h), handles.TypeRegistryClient)
}

// helm_pull downloads a chart from an HTTP repo or oci:// ref. client_h is
// optional (0 = default client for OCI). opts_json optional — see
// docs/API.md. *out receives {"output":"..."} JSON.
//
//export helm_pull
func helm_pull(clientH C.uint64_t, chartRef *C.char, optsJSON *C.char, out **C.char, errOut **C.char) C.int32_t {
	return stringResult(out, errOut, func() (string, error) {
		if err := requireArgs("chart_ref", chartRef); err != nil {
			return "", err
		}
		clientObj, err := registryClientOrNil(clientH)
		if err != nil {
			return "", err
		}
		opts, err := wrapper.ParsePullOptions(optionalGoString(optsJSON))
		if err != nil {
			return "", err
		}
		return wrapper.PullChart(clientObj, C.GoString(chartRef), opts)
	})
}

// helm_push uploads a chart archive to an oci:// remote. client_h optional
// (0 = default client). opts_json optional. *out receives {"output":"..."}.
//
//export helm_push
func helm_push(clientH C.uint64_t, chartPath *C.char, remote *C.char, optsJSON *C.char, out **C.char, errOut **C.char) C.int32_t {
	return stringResult(out, errOut, func() (string, error) {
		if err := requireArgs("chart_path and remote", chartPath, remote); err != nil {
			return "", err
		}
		clientObj, err := registryClientOrNil(clientH)
		if err != nil {
			return "", err
		}
		opts, err := wrapper.ParsePushOptions(optionalGoString(optsJSON))
		if err != nil {
			return "", err
		}
		return wrapper.PushChart(clientObj, C.GoString(chartPath), C.GoString(remote), opts)
	})
}

// helm_repo_index_download fetches a chart repository's index.yaml and writes
// it as JSON into *out. opts_json optional.
//
//export helm_repo_index_download
func helm_repo_index_download(repoURL *C.char, optsJSON *C.char, out **C.char, errOut **C.char) C.int32_t {
	return stringResult(out, errOut, func() (string, error) {
		if err := requireArgs("repo_url", repoURL); err != nil {
			return "", err
		}
		opts, err := wrapper.ParseRepoIndexOptions(optionalGoString(optsJSON))
		if err != nil {
			return "", err
		}
		return wrapper.DownloadRepoIndex(C.GoString(repoURL), opts)
	})
}
