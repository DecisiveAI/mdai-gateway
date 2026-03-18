package variables

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHTTPError(t *testing.T) {
	err := HTTPError{Msg: "not found", Status: 404}

	assert.Equal(t, "not found", err.Error())
	assert.Equal(t, 404, err.HTTPStatus())
}
