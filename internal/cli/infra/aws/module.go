// MonVM <https://monvm.dev>
// Copyright The MonVM Authors
// SPDX-License-Identifier: Apache-2.0

package aws

import "embed"

//go:embed *.tf *.tftpl *.sh
var Module embed.FS
