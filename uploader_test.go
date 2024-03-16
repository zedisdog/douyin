package douyin

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestUploader(t *testing.T) {
	//key=value;key=value;key=value
	cookies := ""
	u := NewUploader(ShowWindow(true))
	cookies, err := u.Upload(&Task{
		PublishId:   "123",
		VideoUrl:    "", //视频下载链接或本地路径
		Cookies:     cookies,
		VideoTitle:  "123",
		VideoDesc:   "456",
		AppUrl:      "",
		PublishTime: "",
	}, "")
	require.NotEmpty(t, cookies)
	require.NoError(t, err)
}
