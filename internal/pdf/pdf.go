package pdf

import (
	"context"
	"errors"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/device"
	pgt "github.com/nicoxiang/geektime-downloader/internal/pkg/geektime"
)

// ErrGeekTimeRateLimit ...
var ErrGeekTimeRateLimit = errors.New("已触发限流, 你可以选择重新登录/重新获取 cookie, 或者稍后再试, 然后生成剩余的文章")

// PrintArticlePageToPDF use chromedp to print article page and save
func PrintArticlePageToPDF(ctx context.Context, aid int, filename string, cookies []*http.Cookie, downloadComments bool) error {
	rateLimit := false
	// new tab
	ctx, cancel := chromedp.NewContext(ctx)
	defer cancel()

	ctx, cancel = context.WithTimeout(ctx, time.Minute)
	defer cancel()

	chromedp.ListenTarget(ctx, func(ev interface{}) {
		switch responseReceivedEvent := ev.(type) {
		case *network.EventResponseReceived:
			response := responseReceivedEvent.Response
			if response.URL == pgt.GeekBang+"/serv/v1/article" && response.Status == 451 {
				rateLimit = true
				cancel()
			}
		}
	})

	var buf []byte
	err := chromedp.Run(ctx,
		chromedp.Tasks{
			chromedp.Emulate(device.IPadPro11),
			setCookies(cookies),
			chromedp.Navigate(pgt.GeekBang + `/column/article/` + strconv.Itoa(aid)),
			// wait for loading show
			chromedp.WaitVisible("._loading_wrap_", chromedp.ByQuery),
			// wait for loading disappear
			chromedp.WaitNotPresent("._loading_wrap_", chromedp.ByQuery),
			waitForImagesLoad(),
			hideRedundantElements(downloadComments),
			printToPDF(&buf),
		},
	)

	if err != nil {
		if rateLimit {
			return ErrGeekTimeRateLimit
		}
		return err
	}

	if err := ioutil.WriteFile(filename, buf, os.ModePerm); err != nil {
		return err
	}
	return nil
}

func setCookies(cookies []*http.Cookie) chromedp.ActionFunc {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		expr := cdp.TimeSinceEpoch(time.Now().Add(180 * 24 * time.Hour))

		for _, c := range cookies {
			err := network.SetCookie(c.Name, c.Value).WithExpires(&expr).WithDomain(pgt.GeekBangCookieDomain).WithHTTPOnly(true).Do(ctx)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func hideRedundantElements(downloadComments bool) chromedp.ActionFunc {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		s :=
			`
			var openAppdiv = document.getElementsByClassName('openApp')[0];
			if(openAppdiv){
				openAppdiv.parentNode.parentNode.parentNode.style.display="none";
			}
			var audioPlayer = document.querySelector('div[class^="ColumnArticleMiniAudioPlayer"]');
			if(audioPlayer){
				audioPlayer.style.display="none"
			}
			var leadsWrapper = document.querySelector('div[class^="leads-wrapper"]');
			if(leadsWrapper){
				leadsWrapper.style.display="none";
			}
			var unPreviewImage = document.querySelector('img[alt="unpreview"]');
			if(unPreviewImage){
				unPreviewImage.style.display="none"
			}
			var gotoColumn = document.querySelector('div[class^="Index_articleColumn"]');
			if(gotoColumn){
				gotoColumn.style.display="none"
			}
			var favBtn = document.querySelector('div[class*="Index_favBtn"]');
			if(favBtn){
				favBtn.style.display="none"
			}
			var likeModule = document.querySelector('div[class^="ArticleLikeModuleMobile"]');
			if(likeModule){
				likeModule.style.display="none"
			}
			var switchBtns = document.querySelector('div[class^="Index_switchBtns"]');
			if(switchBtns){
				switchBtns.style.display="none"
			}
			var writeComment = document.querySelector('div[class*="Index_writeComment"]');
			if(writeComment){
				writeComment.style.display="none"
			}
		`

		hideCommentsExpression :=
			`
			var comments = document.querySelector('div[class^="Index_articleComments"]')
			if(comments){
				comments.style.display="none"
			}
		`
		if !downloadComments {
			s = s + hideCommentsExpression
		}

		_, exp, err := runtime.Evaluate(s).Do(ctx)
		if err != nil {
			return err
		}

		if exp != nil {
			return exp
		}

		return nil
	})
}

func printToPDF(res *[]byte) chromedp.ActionFunc {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		data, _, err := page.PrintToPDF().
			WithMarginTop(0.4).
			WithMarginBottom(0.4).
			WithMarginLeft(0.4).
			WithMarginRight(0.4).
			Do(ctx)
		if err != nil {
			return err
		}
		*res = data
		return nil
	})
}

func waitForImagesLoad() chromedp.ActionFunc {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		return waitFor(ctx, "networkIdle")
	})
}

// waitFor blocks until eventName is received.
// Examples of events you can wait for:
//     init, DOMContentLoaded, firstPaint,
//     firstContentfulPaint, firstImagePaint,
//     firstMeaningfulPaintCandidate,
//     load, networkAlmostIdle, firstMeaningfulPaint, networkIdle
//
// This is not super reliable, I've already found incidental cases where
// networkIdle was sent before load. It's probably smart to see how
// puppeteer implements this exactly.
func waitFor(ctx context.Context, eventName string) error {
	ch := make(chan struct{})
	cctx, cancel := context.WithCancel(ctx)
	chromedp.ListenTarget(cctx, func(ev interface{}) {
		switch e := ev.(type) {
		case *page.EventLifecycleEvent:
			if e.Name == eventName {
				cancel()
				close(ch)
			}
		}
	})

	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
