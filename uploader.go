package douyin

import (
	"context"
	"errors"
	"fmt"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	ErrUnknown         = errors.New("unknown")
	ErrNoLogin         = errors.New("no login")
	ErrAlreadyUploaded = errors.New("video already uploaded")
)

func ShowWindow(enable bool) func(*Uploader) {
	return func(uploader *Uploader) {
		uploader.showWindow = enable
	}
}

func NewUploader(opts ...func(*Uploader)) *Uploader {
	u := &Uploader{
		logger: slog.Default(),
	}
	for _, opt := range opts {
		opt(u)
	}
	return u
}

type Uploader struct {
	showWindow bool
	logger     *slog.Logger
	once       sync.Once
	ctx        context.Context
	cancel     func()
}

func (u *Uploader) Close() {
	u.once.Do(func() {
		if u.cancel != nil {
			u.cancel()
		}
		u.cancel = nil
		u.ctx = nil
	})
}

func (u *Uploader) Upload(task *Task, proxy string) (newCookies string, err error) {
	err = u.bootBrowser(proxy)
	if err != nil {
		return
	}
	defer func() {
		newCookies = u.getCookie(u.ctx)
		u.Close()
	}()

	var path string
	err = chromedp.Run(u.ctx,
		u.setCookies(task.Cookies),                 //设置cookies
		u.check(task.VideoTitle, task.VideoDesc),   // 判断是否掉线和是否已经存在要上传的视频
		u.download(task.VideoUrl, &path),           // 下载文件
		u.uploadFile(&path),                        // 上传文件
		u.setDesc(task.VideoTitle, task.VideoDesc), // 设置描述
		u.setAppLink(task.AppUrl),                  // 设置小程序链接
		u.setPublishTime(task.PublishTime),         // 设置发布时间
		u.submit(),                                 // 提交发布并等待完成
	)

	if err != nil && !errors.Is(err, ErrAlreadyUploaded) {
		PicWrite(u.ctx, "publish_shot/"+task.PublishId+"_shot.png")
		slog.Error("upload failed", "error", err)
	} else {
		err = nil
	}

	return
}

func (u *Uploader) submit() chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) (err error) {
		u.logger.Info("publish")
		for {
			select {
			case <-ctx.Done():
				return
			default:
				var url string
				err = chromedp.Run(ctx,
					chromedp.Sleep(3*time.Second),
					chromedp.Location(&url),
					chromedp.ActionFunc(func(ctx context.Context) error {
						if strings.Contains(url, "creator-micro/content/manage") {
							return nil
						} else {
							return errors.New("not jump")
						}
					}),
				)
				if err != nil {
					u.logger.Info("submit")
					c, _ := context.WithTimeout(ctx, 3*time.Second)
					_ = chromedp.Run(c,
						chromedp.Click("#root > div > div > div[class^=content-body] > div[class^=form] > div[class^=content-confirm-container] > button"),
					)
				} else {
					return
				}
			}
		}
	})
}

func (u *Uploader) setPublishTime(t string) chromedp.Action {
	if t == "" {
		return chromedp.Tasks{}
	}
	var nodes []*cdp.Node
	return chromedp.Tasks{
		u.log("set publish time"),
		chromedp.Click("#root > div > div > div > div > div > div[class^=schedule-part] > div[class^=row] > div"),
		chromedp.WaitVisible("#root > div > div > div[class^=content-body] > div[class^=form] > div[class^=publish-settings] > div[class^=schedule-part] > div[class^=row] > div[class^=date-picker] > div > div > div > input"),
		chromedp.Nodes("#root > div > div > div[class^=content-body] > div[class^=form] > div[class^=publish-settings] > div[class^=schedule-part] > div[class^=row] > div[class^=date-picker] > div > div > div > input", &nodes),
		chromedp.ActionFunc(func(ctx context.Context) (err error) {
			if len(nodes) > 0 {
				return chromedp.Run(ctx,
					chromedp.MouseClickNode(nodes[0], chromedp.ClickCount(3)),
				)
			} else {
				return errors.New("no nodes selected")
			}
		}),
		chromedp.SendKeys("#root > div > div > div[class^=content-body] > div[class^=form] > div[class^=publish-settings] > div[class^=schedule-part] > div[class^=row] > div[class^=date-picker] > div > div > div > input", t),
		chromedp.Sleep(time.Second),
	}
}

func (u *Uploader) setAppLink(appUrl string) chromedp.Action {
	if appUrl == "" {
		return chromedp.Tasks{}
	}

	js := `(function() {
	let options = document.querySelectorAll("div.semi-portal > div.semi-portal-inner > div.semi-popover-wrapper > div.semi-popover > div.semi-popover-content > div.select-dropdown > div > div.semi-select-option")
	for(let item of options) {
		if(item.querySelector("div:nth-child(2)").innerText === "小程序") {
			item.click()
			return true
		}
	}
	return false
})()`

	return chromedp.Tasks{
		u.log("select mini app option"),
		chromedp.Evaluate(`document.querySelectorAll("div[class^=tooltip]").forEach(function(item){item.style.display = 'none'})`, nil),
		chromedp.Sleep(time.Second),
		chromedp.Evaluate(`document.querySelector("#root > div > div > div[class^=content-body] > div[class^=form] > div[class^=anchor-part] > div[class^=anchor-container] > div.semi-select.semi-select-single").click()`, nil),
		chromedp.Sleep(time.Second),
		u.log("wait dropdown"),
		chromedp.WaitVisible("div.semi-portal > div.semi-portal-inner > div.semi-popover-wrapper > div.semi-popover > div.semi-popover-content > div.select-dropdown"),
		chromedp.Sleep(time.Second),
		chromedp.ActionFunc(func(ctx context.Context) (err error) {
			clicked := false
			err = chromedp.Evaluate(js, &clicked).Do(ctx)
			if err != nil {
				return
			}
			if !clicked {
				err = errors.New("mini app option not found")
				return
			}
			return
		}),
		chromedp.Sleep(1 * time.Second),

		u.log("fill app link"),
		chromedp.Evaluate(`document.querySelectorAll("div[class^=tooltip]").forEach(function(item){item.style.display = 'none'})`, nil),
		chromedp.Sleep(1 * time.Second),
		chromedp.Evaluate(`document.querySelector("#root > div > div > div[class^=content-body] > div[class^=form] > div[class^=anchor-part] > div[class^=anchor-container] > div[class^=anchor-component] > div[class^=anchor-item] > div.semi-select-filterable").click()`, nil),
		chromedp.Sleep(1 * time.Second),
		chromedp.SendKeys(
			"#root > div > div > div[class^=content-body] > div[class^=form] > div[class^=anchor-part] > div[class^=anchor-container] > div[class^=anchor-component] > div[class^=anchor-item] > div.semi-select-filterable > div.semi-select-selection > div > div > input.semi-input-default",
			appUrl,
		),
		u.log("wait app data"),
		chromedp.ActionFunc(func(ctx context.Context) (err error) {
			c, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			err = chromedp.Run(c,
				chromedp.WaitVisible("body > div.semi-portal > div.semi-portal-inner > div.semi-popover-wrapper > div.semi-popover > div.semi-popover-content > div > div.semi-select-option-list[role=listbox] > div.semi-select-option.option:nth-child(1)"),
			)
			if err != nil {
				err = errors.New("mini app select not open")
				return
			}
			return
		}),
		chromedp.Click("body > div.semi-portal > div.semi-portal-inner > div.semi-popover-wrapper > div.semi-popover > div.semi-popover-content > div > div.semi-select-option-list[role=listbox] > div.semi-select-option.option:nth-child(1)"),
		chromedp.Sleep(time.Second),
	}
}

func (u *Uploader) bootBrowser(proxy string) (err error) {
	opt := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.WindowSize(1440, 900),
		chromedp.Flag("user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36"),
		chromedp.Flag("headless", !u.showWindow),
	)
	if proxy != "" {
		opt = append(opt, chromedp.ProxyServer(proxy))
	}
	var ctx context.Context
	ctx, u.cancel = chromedp.NewExecAllocator(context.Background(), opt...)
	u.ctx, _ = chromedp.NewContext(ctx)
	err = chromedp.Run(u.ctx,
		HeadlessFake(),
		chromedp.Sleep(time.Second),
	)
	u.once = sync.Once{}
	return
}

func (u *Uploader) check(title, desc string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) (err error) {
		err = chromedp.Run(ctx,
			u.log("check login"),
			chromedp.Navigate("https://creator.douyin.com/creator-micro/content/manage"),
			chromedp.Sleep(3*time.Second),
		)
		if err != nil {
			return
		}

		for {
			c, _ := context.WithTimeout(ctx, 3*time.Second)
			err = chromedp.Run(c,
				chromedp.WaitVisible("#root > div.card-container-creator-layout > div[class^=card] > div[class^=title] > div[class^=card-head] > div:nth-child(1)"),
			)
			if err != nil {
				c, _ := context.WithTimeout(ctx, 3*time.Second)
				err = chromedp.Run(c,
					chromedp.WaitVisible("#root > div.creator-container > section.semi-layout > header.creator-header > div.creator-header-wrapper > div.creator-header-nav > div.semi-navigation > div.semi-navigation-inner > div.semi-navigation-footer > span.login"),
				)
				if err != nil {
					continue
				} else {
					err = ErrNoLogin
					return
				}
			} else {
				break
			}
		}

		js := `
(function (title) {
	let list = document.querySelectorAll("#root > div.card-container-creator-layout > div[class^=card] > div:nth-child(3) > div:nth-child(2) > div[class^=content-body] > div[class^=video-card]")
	for (let item of list) {
		if (typeof item.querySelector !== "function") {
			continue
		}
		if (item.querySelector("div[class^=video-card-info] > div:nth-child(1) > div[class^=info-title-text]").innerText === title) {
			return true
		}
	}

	return false
})("%s")
`
		err = chromedp.Run(ctx,
			chromedp.WaitVisible("#root > div.card-container-creator-layout > div[class^=card] > div:nth-child(3) > div:nth-child(2) > div[class^=content-body]"),
		)
		if err != nil {
			return
		}

		name := strings.Trim(fmt.Sprintf("%s %s", title, desc), " ")

		c, _ := context.WithTimeout(ctx, 10*time.Second)
		_ = chromedp.Run(c,
			u.log(fmt.Sprintf("check if [%s] is exists", name)),
			chromedp.WaitVisible("#root > div.card-container-creator-layout > div[class^=card] > div:nth-child(3) > div:nth-child(2) > div[class^=content-body] > div[class^=video-card]"),
		)

		var exists bool
		err = chromedp.Run(ctx,
			chromedp.Evaluate(fmt.Sprintf(js, name), &exists),
		)
		if err != nil {
			return
		}
		if exists {
			err = ErrAlreadyUploaded
			return
		}

		return
	})
}

func (u *Uploader) getCookie(ctx context.Context) (cookies string) {
	err := chromedp.Run(ctx,
		u.log("get cookies"),
		chromedp.ActionFunc(func(ctx context.Context) (err error) {
			params, err := network.GetCookies().Do(ctx)
			if err != nil {
				return
			}

			c := make([]string, 0, len(cookies))
			for _, cookie := range params {
				c = append(c, fmt.Sprintf("%s=%s", cookie.Name, cookie.Value))
			}
			cookies = strings.Join(c, ";")
			return
		}),
	)
	if err != nil {
		slog.Error("get Cookies from chrome failed", "error", err)
	}

	return
}

func (u *Uploader) setCookies(cookies string) chromedp.Action {
	return chromedp.Tasks{
		u.log("set cookies"),
		network.SetCookies(parseCookieRaw(cookies)),
	}
}

func (u *Uploader) log(msg string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		u.logger.Info(msg)
		return nil
	})
}

func (u *Uploader) download(fileUrl string, path *string) chromedp.Action {
	return chromedp.Tasks{
		u.log("download file"),
		chromedp.ActionFunc(func(ctx context.Context) (err error) {
			if !strings.HasPrefix(fileUrl, "http") {
				*path = fileUrl
				return
			}
			resp, err := http.Get(fileUrl)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			content, err := io.ReadAll(resp.Body)
			if err != nil {
				return
			}
			if resp.StatusCode != http.StatusOK {
				err = errors.New(fmt.Sprintf("download file failed, status %d, with body: %s", resp.StatusCode, string(content)))
			} else {
				var (
					fileName = filepath.Base(fileUrl)
					f        *os.File
				)
				f, err = os.CreateTemp("", "*-"+fileName)
				if err != nil {
					err = fmt.Errorf("open file for write file failed: %w", err)
					return
				}
				defer f.Close()

				*path = f.Name()

				_, err = f.Write(content)
				if err != nil {
					err = fmt.Errorf("write file failed: %w", err)
					return
				}
			}
			return
		}),
	}
}

func (u *Uploader) uploadFile(path *string) chromedp.Action {
	return chromedp.Tasks{
		u.log("upload file"),
		chromedp.ActionFunc(func(ctx context.Context) error {
			page.SetInterceptFileChooserDialog(true).Do(ctx)
			lctx, lcancel := context.WithCancel(ctx)
			chromedp.ListenTarget(lctx, func(ev interface{}) {
				switch ev.(type) {
				case *page.EventFileChooserOpened:
					go func() {
						err := chromedp.Run(ctx,
							u.log("set upload filepath"),
							chromedp.SetUploadFiles(
								"#root > div > div > div.semi-tabs.semi-tabs-top > div > div.semi-tabs-pane-active.semi-tabs-pane > div > div > div > label > input",
								[]string{*path},
							),
							page.SetInterceptFileChooserDialog(false),
						)
						if err != nil {
							slog.Error("set upload filepath failed", "error", err)
						}
					}()
					lcancel()
				}
			})
			return nil
		}),
		chromedp.Navigate("https://creator.douyin.com/creator-micro/content/upload"),
		chromedp.Click("#root > div > div > div.semi-tabs > div.semi-tabs-content > div.semi-tabs-pane > div.semi-tabs-pane-motion-overlay > div[class^=container] > div[class^=upload] > label[class^=upload-btn]"),
		chromedp.WaitVisible("#root > div > div > div > div > div > div > div > div > div:nth-child(1) > div > div > input.semi-input-default"), //等表单页的标题输入框
	}
}

func (u *Uploader) setDesc(title, desc string) chromedp.Action {
	return chromedp.Tasks{
		u.log("set desc"),
		chromedp.Sleep(1 * time.Second),
		chromedp.SendKeys("#root > div > div > div > div > div > div > div > div > div:nth-child(1) > div > div > input.semi-input-default", title),
		chromedp.Sleep(1 * time.Second),
		chromedp.SendKeys("#root > div > div > div > div > div > div > div > div > div.outerdocbody.editor-kit-outer-container > div", desc),
	}
}
