// SPDX-License-Identifier: MIT

package configfile

import (
	"encoding/json"
	"errors"

	"github.com/BurntSushi/toml"
)

func asTOMLParseError(err error, pe *toml.ParseError) bool { return errors.As(err, pe) }
func asTypeError(err error, te **json.UnmarshalTypeError) bool {
	return errors.As(err, te)
}
func asSyntaxError(err error, se **json.SyntaxError) bool { return errors.As(err, se) }
