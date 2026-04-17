package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEnvFile_ValidFile(t *testing.T) {
	content := `# API keys
API_KEY=sk-123456
DB_HOST=localhost
DB_PORT=5432
`
	path := writeTempEnvFile(t, content)
	m, err := ParseEnvFile(path)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"API_KEY": "sk-123456",
		"DB_HOST": "localhost",
		"DB_PORT": "5432",
	}, m)
}

func TestParseEnvFile_CommentsAndBlankLines(t *testing.T) {
	content := `
# This is a comment
  # Indented comment

KEY1=val1


KEY2=val2
# trailing comment
`
	path := writeTempEnvFile(t, content)
	m, err := ParseEnvFile(path)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"KEY1": "val1",
		"KEY2": "val2",
	}, m)
}

func TestParseEnvFile_DoubleQuotedValue(t *testing.T) {
	content := `SECRET="my secret value"
PLAIN=no-quotes
`
	path := writeTempEnvFile(t, content)
	m, err := ParseEnvFile(path)
	require.NoError(t, err)
	assert.Equal(t, "my secret value", m["SECRET"])
	assert.Equal(t, "no-quotes", m["PLAIN"])
}

func TestParseEnvFile_SingleQuotedValue(t *testing.T) {
	content := `TOKEN='abc123'
`
	path := writeTempEnvFile(t, content)
	m, err := ParseEnvFile(path)
	require.NoError(t, err)
	assert.Equal(t, "abc123", m["TOKEN"])
}

func TestParseEnvFile_EmptyValue(t *testing.T) {
	content := `EMPTY=
ALSO_EMPTY=""
`
	path := writeTempEnvFile(t, content)
	m, err := ParseEnvFile(path)
	require.NoError(t, err)
	assert.Equal(t, "", m["EMPTY"])
	assert.Equal(t, "", m["ALSO_EMPTY"])
}

func TestParseEnvFile_ValueWithEquals(t *testing.T) {
	content := `DATABASE_URL=postgres://user:pass@host/db?sslmode=require
`
	path := writeTempEnvFile(t, content)
	m, err := ParseEnvFile(path)
	require.NoError(t, err)
	assert.Equal(t, "postgres://user:pass@host/db?sslmode=require", m["DATABASE_URL"])
}

func TestParseEnvFile_ExportPrefixSkipped(t *testing.T) {
	content := `export FOO=bar
export BAZ=qux
`
	path := writeTempEnvFile(t, content)
	m, err := ParseEnvFile(path)
	require.NoError(t, err)
	assert.Equal(t, "bar", m["FOO"])
	assert.Equal(t, "qux", m["BAZ"])
}

func TestParseEnvFile_MissingFile(t *testing.T) {
	_, err := ParseEnvFile("/nonexistent/path/to/file.env")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "env_file")
}

func TestParseEnvFile_EmptyFile(t *testing.T) {
	path := writeTempEnvFile(t, "")
	m, err := ParseEnvFile(path)
	require.NoError(t, err)
	assert.Empty(t, m)
}

func TestParseEnvFile_MalformedLineSkipped(t *testing.T) {
	content := `GOOD=value
MALFORMED_NO_EQUALS
ALSO_GOOD=yes
`
	path := writeTempEnvFile(t, content)
	m, err := ParseEnvFile(path)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"GOOD":      "value",
		"ALSO_GOOD": "yes",
	}, m)
}

func TestParseEnvFile_WhitespaceAroundKeyValue(t *testing.T) {
	content := `  KEY1 = value1
KEY2=  value2
`
	path := writeTempEnvFile(t, content)
	m, err := ParseEnvFile(path)
	require.NoError(t, err)
	assert.Equal(t, "value1", m["KEY1"])
	assert.Equal(t, "value2", m["KEY2"])
}

func writeTempEnvFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.env")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	return path
}
