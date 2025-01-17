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

package terragruntretriever

import (
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tofuutils/tenv/config"
	sha256check "github.com/tofuutils/tenv/pkg/check/sha256"
	"github.com/tofuutils/tenv/pkg/download"
	"github.com/tofuutils/tenv/pkg/github"
	htmlretriever "github.com/tofuutils/tenv/versionmanager/retriever/html"
)

const (
	baseFileName  = "terragrunt_"
	gruntworkName = "gruntwork-io"
	Name          = "terragrunt"
)

type TerragruntRetriever struct {
	conf *config.Config
}

func NewTerragruntRetriever(conf *config.Config) *TerragruntRetriever {
	return &TerragruntRetriever{conf: conf}
}

func (r *TerragruntRetriever) InstallRelease(versionStr string, targetPath string) error {
	r.conf.InitRemoteConf()

	tag := versionStr
	// assume that terragrunt tags start with a 'v'
	if tag[0] != 'v' {
		tag = "v" + versionStr
	}

	var err error
	var assetURLs []string
	fileName, shaFileName := buildAssetNames()
	if r.conf.Tg.GetInstallMode() == htmlretriever.InstallModeDirect {
		baseAssetURL, err2 := url.JoinPath(r.conf.Tg.GetRemoteURL(), gruntworkName, Name, github.Releases, github.Download, tag) //nolint
		if err2 != nil {
			return err2
		}

		assetURLs, err = htmlretriever.BuildAssetURLs(baseAssetURL, fileName, shaFileName)
	} else {
		assetURLs, err = github.AssetDownloadURL(tag, []string{fileName, shaFileName}, r.conf.Tg.GetRemoteURL(), r.conf.GithubToken, r.conf.Verbose)
	}
	if err != nil {
		return err
	}

	urlTranformer := download.UrlTranformer(r.conf.Tg.GetRewriteRule())
	assetURLs, err = download.ApplyUrlTranformer(urlTranformer, assetURLs...)
	if err != nil {
		return err
	}

	data, err := download.Bytes(assetURLs[0], r.conf.Verbose)
	if err != nil {
		return err
	}

	dataSums, err := download.Bytes(assetURLs[1], r.conf.Verbose)
	if err != nil {
		return err
	}

	if err = sha256check.Check(data, dataSums, fileName); err != nil {
		return err
	}

	err = os.MkdirAll(targetPath, 0755)
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(targetPath, Name), data, 0755)
}

func (r *TerragruntRetriever) ListReleases() ([]string, error) {
	r.conf.InitRemoteConf()

	if r.conf.Tg.GetListMode() == htmlretriever.ListModeHTML {
		baseURL, err := url.JoinPath(r.conf.Tg.GetListURL(), gruntworkName, Name, github.Releases, github.Download) //nolint
		if err != nil {
			return nil, err
		}

		return htmlretriever.ListReleases(baseURL, r.conf.Tg.Data, r.conf.Verbose)
	}

	return github.ListReleases(r.conf.Tg.GetListURL(), r.conf.GithubToken, r.conf.Verbose)
}

func buildAssetNames() (string, string) {
	var nameBuilder strings.Builder
	nameBuilder.WriteString(baseFileName)
	nameBuilder.WriteString(runtime.GOOS)
	nameBuilder.WriteByte('_')
	nameBuilder.WriteString(runtime.GOARCH)
	if runtime.GOOS == "windows" {
		nameBuilder.WriteString(".exe")
	}

	return nameBuilder.String(), "SHA256SUMS"
}
