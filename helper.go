package douyin

import (
	"context"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func PicWrite(ctx context.Context, filename string) {
	var img []byte
	e := chromedp.Run(ctx,
		chromedp.FullScreenshot(&img, 100),
	)
	if e != nil {
		slog.Error("FullScreenshot failed", "error", e)
	} else {
		abs, err := filepath.Abs(filename)
		if err != nil {
			slog.Error("FullScreenshot failed: filename is invalid", "error", e)
			return
		}
		err = os.MkdirAll(filepath.Dir(abs), os.ModeDir)
		if err != nil {
			slog.Error("FullScreenshot failed: create dir failed", "path", filepath.Dir(abs), "error", e)
			return
		}
		var f *os.File
		f, e = os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.ModePerm)
		if e != nil {
			slog.Error("open img failed", "error", e)
		} else {
			_, e = f.Write(img)
			if e != nil {
				slog.Error("write img failed", "error", e)
			}
			f.Close()
			slog.Info("pic write")
		}
	}
}

func HeadlessFake() chromedp.Action {
	overiteJs := `
		// overwrite the 'languages' property to use a custom getter
		Object.defineProperty(navigator, 'languages', {
		  get: function() {
			return ['en-US', 'en'];
		  },
		});
		
		// overwrite the 'plugins' property to use a custom getter
		Object.defineProperty(navigator, 'plugins', {
		  get: function() {
			// this just needs to have 'length > 0', but we could mock the plugins too
			return [1, 2, 3, 4, 5];
		  },
		});

		const getParameter = WebGLRenderingContext.getParameter;
		WebGLRenderingContext.prototype.getParameter = function(parameter) {
		  // UNMASKED_VENDOR_WEBGL
		  if (parameter === 37445) {
			return 'Intel Open Source Technology Center';
		  }
		  // UNMASKED_RENDERER_WEBGL
		  if (parameter === 37446) {
			return 'Mesa DRI Intel(R) Ivybridge Mobile ';
		  }
		
		  return getParameter(parameter);
		};

		['height', 'width'].forEach(property => {
		  // store the existing descriptor
		  const imageDescriptor = Object.getOwnPropertyDescriptor(HTMLImageElement.prototype, property);
		
		  // redefine the property with a patched descriptor
		  Object.defineProperty(HTMLImageElement.prototype, property, {
			...imageDescriptor,
			get: function() {
			  // return an arbitrary non-zero dimension if the image failed to load
			  if (this.complete && this.naturalHeight == 0) {
				return 20;
			  }
			  // otherwise, return the actual dimension
			  return imageDescriptor.get.apply(this);
			},
		  });
		});
	`
	return chromedp.Evaluate(overiteJs, nil)
}

// parseCookieRaw 读取key=value;key2=value2的这种cookie
func parseCookieRaw(rawCookies string) (cookies []*network.CookieParam) {
	rawCookies = strings.Replace(rawCookies, " ", "", -1)
	tmp := strings.Split(rawCookies, ";")

	cookies = make([]*network.CookieParam, 0, len(tmp))
	// create cookie expiration
	expr := cdp.TimeSinceEpoch(time.Now().Add(2 * time.Hour))
	for _, item := range tmp {
		if item == "" {
			continue
		}
		t := strings.SplitN(item, "=", 2)
		cookies = append(cookies, &network.CookieParam{
			Name:    t[0],
			Value:   t[1],
			Domain:  "creator.douyin.com",
			Path:    "/",
			Expires: &expr,
		})
	}

	return
}
