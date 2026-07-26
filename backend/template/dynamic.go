package template

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// dynamicVar 处理 {{$name}} 动态变量，命名对齐 Postman（docs/request-lifecycle.md §2.2）
func dynamicVar(name string) (string, bool) {
	switch name {
	case "$guid", "$uuid", "$randomUUID":
		return uuid.NewString(), true
	case "$timestamp":
		return strconv.FormatInt(time.Now().Unix(), 10), true
	case "$isoTimestamp":
		return time.Now().UTC().Format(time.RFC3339), true
	case "$randomInt":
		return strconv.FormatInt(randInt(1000), 10), true
	case "$randomBoolean":
		if randInt(2) == 0 {
			return "false", true
		}
		return "true", true
	case "$randomEmail":
		return fmt.Sprintf("user%d@example.com", randInt(100000)), true
	case "$randomFirstName":
		return pick(firstNames), true
	case "$randomLastName":
		return pick(lastNames), true
	case "$randomIP":
		return fmt.Sprintf("%d.%d.%d.%d", randInt(256), randInt(256), randInt(256), randInt(256)), true
	case "$randomPort":
		return strconv.FormatInt(randInt(65535-1024)+1024, 10), true
	case "$randomColor":
		return pick(colors), true
	case "$randomPrice":
		return fmt.Sprintf("%d.%02d", randInt(1000), randInt(100)), true
	default:
		return "", false
	}
}

var firstNames = []string{"Alice", "Bob", "Carol", "David", "Eve", "Frank", "Grace", "Henry"}
var lastNames = []string{"Smith", "Johnson", "Brown", "Lee", "Wang", "Garcia", "Miller", "Davis"}
var colors = []string{"red", "green", "blue", "yellow", "purple", "orange", "cyan", "magenta"}

func randInt(n int64) int64 {
	v, err := rand.Int(rand.Reader, big.NewInt(n))
	if err != nil {
		return 0
	}
	return v.Int64()
}

func pick(list []string) string { return list[randInt(int64(len(list)))] }
