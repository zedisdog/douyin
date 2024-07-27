# 抖音视频上传自动化

通过chromedp库对视频上传做自动化

## 使用
1. 登录抖音创作者中心https://creator.douyin.com
2. 首页获取live_status接口请求中的cookie
3. 使用uploader_test.go中的示例代码运行

## 特性
- [x] 支持代理
- [x] 简单重复检测(检测第一页是否已经存在当前要发的视频)
- [x] 支持视频http下载链接
- [x] 支持指定本地路径
- [x] 支持设置作品标题和描述
- [x] 支持设置小程序链接
- [x] 支持设置发布时间
- [x] 失败时截图
- [ ] 过短信验证(不是每次都有短信验证)
- [ ] 对表单其他字段的支持
