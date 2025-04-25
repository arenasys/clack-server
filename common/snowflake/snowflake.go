package snowflake

import (
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/bwmarrin/snowflake"
)

var node *snowflake.Node

var count int64 = 10
var guard = sync.Mutex{}

func init() {
	var err error
	node, err = snowflake.NewNode(1)
	if err != nil {
		panic(err)
	}

	time.Sleep(2 * time.Millisecond)
}

type Snowflake int64

func New() Snowflake {
	/*guard.Lock()
	defer guard.Unlock()
	count += 1
	return Snowflake(count)*/

	return Snowflake(node.Generate().Int64())
}

func Parse(id Snowflake) (int64, int64, int64) {
	s := snowflake.ID(id)

	return s.Time(), s.Node(), s.Step()
}

func IsValid(id string) bool {
	_, err := snowflake.ParseString(id)
	if err != nil {
		return false
	}
	return true
}

func (s Snowflake) MarshalJSON() ([]byte, error) {
	str := strconv.FormatInt(int64(s), 10)
	return json.Marshal(str)
}

func (s *Snowflake) UnmarshalJSON(data []byte) error {
	var str string
	var num int64
	if err := json.Unmarshal(data, &str); err != nil {
		if err2 := json.Unmarshal(data, &num); err2 != nil {
			return fmt.Errorf("failed to unmarshal snowflake: %v", err)
		} else {
			*s = Snowflake(num)
			return nil
		}
	}

	val, err := strconv.ParseInt(str, 10, 64)
	if err != nil {
		return fmt.Errorf("failed to parse snowflake: %v", err)
	}

	*s = Snowflake(val)
	return nil
}
