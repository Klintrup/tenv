/*
 *
 * Copyright 2024 tofuutils authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package terraformretriever

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"

	"github.com/tofuutils/tenv/config"
	"github.com/tofuutils/tenv/pkg/apimsg"
	pgpcheck "github.com/tofuutils/tenv/pkg/check/pgp"
	sha256check "github.com/tofuutils/tenv/pkg/check/sha256"
	"github.com/tofuutils/tenv/pkg/download"
	"github.com/tofuutils/tenv/pkg/zip"
	htmlretriever "github.com/tofuutils/tenv/versionmanager/retriever/html"
)

const (
	publicKeyURL = "https://www.hashicorp.com/.well-known/pgp-key.txt"

	baseFileName = "terraform_"
	indexJson    = "index.json"
	Name         = "terraform"
)

type TerraformRetriever struct {
	conf *config.Config
}

func NewTerraformRetriever(conf *config.Config) *TerraformRetriever {
	return &TerraformRetriever{conf: conf}
}

func (r *TerraformRetriever) InstallRelease(version string, targetPath string) error {
	r.conf.InitRemoteConf()

	// assume that terraform  version do not start with a 'v'
	if version[0] == 'v' {
		version = version[1:]
	}

	baseVersionURL, err := url.JoinPath(r.conf.Tf.GetRemoteURL(), Name, version) //nolint
	if err != nil {
		return err
	}

	var fileName, shaFileName, shaSigFileName, downloadURL, downloadSumsURL, downloadSumsSigURL string
	if r.conf.Tf.GetInstallMode() == htmlretriever.InstallModeDirect {
		fileName, shaFileName, shaSigFileName = buildAssetNames(version)
		assetURLs, err := htmlretriever.BuildAssetURLs(baseVersionURL, fileName, shaFileName, shaSigFileName)
		if err != nil {
			return err
		}

		downloadURL, downloadSumsURL, downloadSumsSigURL = assetURLs[0], assetURLs[1], assetURLs[2]
	} else {
		versionUrl, err := url.JoinPath(baseVersionURL, indexJson) //nolint
		if err != nil {
			return err
		}

		if r.conf.Verbose {
			fmt.Println(apimsg.MsgFetchRelease, versionUrl) //nolint
		}

		value, err := apiGetRequest(versionUrl)
		if err != nil {
			return err
		}

		fileName, downloadURL, shaFileName, shaSigFileName, err = extractAssetUrls(runtime.GOOS, runtime.GOARCH, value)
		if err != nil {
			return err
		}

		assetURLs, err := htmlretriever.BuildAssetURLs(baseVersionURL, shaFileName, shaSigFileName)
		if err != nil {
			return err
		}

		downloadSumsURL, downloadSumsSigURL = assetURLs[0], assetURLs[1]
	}

	urlTranformer := download.UrlTranformer(r.conf.Tf.GetRewriteRule())
	assetURLs, err := download.ApplyUrlTranformer(urlTranformer, downloadURL, downloadSumsURL, downloadSumsSigURL)
	if err != nil {
		return err
	}

	data, err := download.Bytes(assetURLs[0], r.conf.Verbose)
	if err != nil {
		return err
	}

	if err = r.checkSumAndSig(fileName, data, assetURLs[1], assetURLs[2]); err != nil {
		return err
	}

	return zip.UnzipToDir(data, targetPath)
}

func (r *TerraformRetriever) ListReleases() ([]string, error) {
	r.conf.InitRemoteConf()

	baseURL, err := url.JoinPath(r.conf.Tf.GetListURL(), Name) //nolint
	if err != nil {
		return nil, err
	}

	if r.conf.Tf.GetListMode() == htmlretriever.ListModeHTML {
		return htmlretriever.ListReleases(baseURL, r.conf.Tf.Data, r.conf.Verbose)
	}

	releasesURL, err := url.JoinPath(baseURL, indexJson) //nolint
	if err != nil {
		return nil, err
	}

	if r.conf.Verbose {
		fmt.Println(apimsg.MsgFetchAllReleases, releasesURL) //nolint
	}

	value, err := apiGetRequest(releasesURL)
	if err != nil {
		return nil, err
	}

	return extractReleases(value)
}

func (r *TerraformRetriever) checkSumAndSig(fileName string, data []byte, downloadSumsURL string, downloadSumsSigURL string) error {
	dataSums, err := download.Bytes(downloadSumsURL, r.conf.Verbose)
	if err != nil {
		return err
	}

	if err = sha256check.Check(data, dataSums, fileName); err != nil {
		return err
	}

	dataSumsSig, err := download.Bytes(downloadSumsSigURL, r.conf.Verbose)
	if err != nil {
		return err
	}

	var dataPublicKey []byte
	if r.conf.TfKeyPath == "" {
		dataPublicKey, err = download.Bytes(publicKeyURL, r.conf.Verbose)
	} else {
		dataPublicKey, err = os.ReadFile(r.conf.TfKeyPath)
	}

	if err != nil {
		return err
	}

	return pgpcheck.Check(dataSums, dataSumsSig, dataPublicKey)
}

func apiGetRequest(callURL string) (any, error) {
	response, err := http.Get(callURL)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	var value any
	err = json.Unmarshal(data, &value)

	return value, err
}

func buildAssetNames(version string) (string, string, string) {
	var nameBuilder strings.Builder
	nameBuilder.WriteString(baseFileName)
	nameBuilder.WriteString(version)
	nameBuilder.WriteByte('_')
	sumsAssetName := nameBuilder.String() + "SHA256SUMS"

	nameBuilder.WriteString(runtime.GOOS)
	nameBuilder.WriteByte('_')
	nameBuilder.WriteString(runtime.GOARCH)
	nameBuilder.WriteString(".zip")

	return nameBuilder.String(), sumsAssetName, sumsAssetName + ".sig"
}

func extractAssetUrls(searchedOs string, searchedArch string, value any) (string, string, string, string, error) {
	object, _ := value.(map[string]any)
	builds, ok := object["builds"].([]any)
	shaFileName, ok2 := object["shasums"].(string)
	shaSigFileName, ok3 := object["shasums_signature"].(string)
	if !ok || !ok2 || !ok3 {
		return "", "", "", "", apimsg.ErrReturn
	}

	for _, build := range builds {
		object, _ = build.(map[string]any)
		osStr, ok := object["os"].(string)
		archStr, ok2 := object["arch"].(string)
		downloadURL, ok3 := object["url"].(string)
		fileName, ok4 := object["filename"].(string)
		if !ok || !ok2 || !ok3 || !ok4 {
			return "", "", "", "", apimsg.ErrReturn
		}

		if osStr != searchedOs || archStr != searchedArch {
			continue
		}

		return fileName, downloadURL, shaFileName, shaSigFileName, nil
	}

	return "", "", "", "", apimsg.ErrAsset
}

func extractReleases(value any) ([]string, error) {
	object, _ := value.(map[string]any)
	object, ok := object["versions"].(map[string]any)
	if !ok {
		return nil, apimsg.ErrReturn
	}

	releases := make([]string, 0, len(object))
	for version := range object {
		releases = append(releases, version)
	}

	return releases, nil
}
