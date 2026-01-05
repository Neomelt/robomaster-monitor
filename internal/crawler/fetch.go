package crawler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"robomaster-monitor/internal/storage"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

const (
	articleURL        = "https://bbs.robomaster.com/article"
	latestArticleFile = "latest_article.txt"
	cookieFile        = "config/cookies.json"
)

// Article holds the information about a newly found article.
type Article struct {
	Title  string
	URL    string
	Href   string // The unique part of the URL used for history comparison
	Author string
}

// randomDelay 生成随机延迟，模拟人类操作
func randomDelay(min, max time.Duration) chromedp.Action {
	delay := min + time.Duration(rand.Int63n(int64(max-min)))
	return chromedp.Sleep(delay)
}

// smoothScroll 平滑滚动页面，模拟人类浏览行为
func smoothScroll(ctx context.Context) error {
	// 随机滚动几次
	scrolls := 2 + rand.Intn(3) // 2-4次滚动
	for i := 0; i < scrolls; i++ {
		scrollAmount := 200 + rand.Intn(300) // 200-500px
		script := fmt.Sprintf(`window.scrollBy({top: %d, behavior: 'smooth'})`, scrollAmount)
		if err := chromedp.Evaluate(script, nil).Do(ctx); err != nil {
			return err
		}
		time.Sleep(time.Duration(300+rand.Intn(500)) * time.Millisecond)
	}
	return nil
}

// saveCookies 保存 cookies 到文件
func saveCookies(ctx context.Context) error {
	// 获取 cookies
	var cookies []*network.Cookie
	if err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			cookies, err = network.GetCookies().Do(ctx)
			return err
		}),
	); err != nil {
		return err
	}

	// 序列化
	data, err := json.MarshalIndent(cookies, "", "  ")
	if err != nil {
		return err
	}

	// 写入文件
	return os.WriteFile(cookieFile, data, 0644)
}

// loadCookies 从文件加载 cookies
func loadCookies(ctx context.Context) error {
	// 检查文件是否存在
	if _, err := os.Stat(cookieFile); os.IsNotExist(err) {
		return fmt.Errorf("cookie 文件不存在")
	}

	// 读取文件
	data, err := os.ReadFile(cookieFile)
	if err != nil {
		return err
	}

	// 反序列化
	var cookies []*network.Cookie
	if err := json.Unmarshal(data, &cookies); err != nil {
		return err
	}

	if len(cookies) == 0 {
		return fmt.Errorf("cookie 文件为空")
	}

	// 过滤过期 cookies
	var validCookies []*network.Cookie
	now := time.Now().Unix()
	for _, c := range cookies {
		// Expires == 0 通常表示会话 Cookie（浏览器关闭失效），我们保留它
		// 否则检查是否已过期
		if c.Expires == 0 || int64(c.Expires) > now {
			validCookies = append(validCookies, c)
		}
	}

	if len(validCookies) == 0 {
		return fmt.Errorf("所有 cookies 已过期")
	}

	log.Printf("🍪 加载了 %d 个有效 Cookies (总数: %d)", len(validCookies), len(cookies))

	// 设置 cookies
	return chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			for _, cookie := range validCookies {
				// 构造 SetCookieParams
				param := network.SetCookie(cookie.Name, cookie.Value).
					WithDomain(cookie.Domain).
					WithPath(cookie.Path).
					WithSecure(cookie.Secure).
					WithHTTPOnly(cookie.HTTPOnly).
					WithSameSite(cookie.SameSite)

				if cookie.Expires != 0 {
					t := cdp.TimeSinceEpoch(time.Unix(int64(cookie.Expires), 0))
					param = param.WithExpires(&t)
				}

				if err := param.Do(ctx); err != nil {
					return err
				}
			}
			return nil
		}),
	)
}

// Login is a public function to perform the login sequence.
func Login(ctx context.Context, username, password string) error {
	const loginButtonTop = `.loginItem` // 顶部的登录按钮
	const passwordTabSelector = `a[data-usagetag="password_login_tab"]`
	const usernameSelector = `input[name="username"]`
	const passwordSelector = `input[type="password"]`
	const loginButtonSelector = `button[data-usagetag="login_button"]`
	const successSelector = `img.user-header.g-avatar`
	const postLoginLoadSelector = `a.articleItem`

	log.Println("🔐 开始登录流程...")

	// 尝试加载 Cookies
	cookiesLoaded := false
	if err := loadCookies(ctx); err != nil {
		log.Printf("⚠️ Cookies 加载跳过: %v", err)
	} else {
		log.Println("✅ Cookies 加载成功，尝试直接访问...")
		cookiesLoaded = true
	}

	// 导航到首页，模拟真实用户行为
	log.Println("📄 访问论坛首页...")
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1920, 1080),
		chromedp.Navigate(articleURL),
		randomDelay(1*time.Second, 2*time.Second),
		// 设置页面缩放为90%
		chromedp.Evaluate(`document.body.style.zoom = '90%'`, nil),
	)
	if err != nil {
		return fmt.Errorf("导航失败: %w", err)
	}

	var isLoggedIn bool
	// 只有在成功加载 Cookie 的情况下才检查登录状态
	if cookiesLoaded {
		// 检查是否已经登录（通过检查头像元素）
		ctxWithTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		err = chromedp.Run(ctxWithTimeout,
			chromedp.WaitVisible(successSelector, chromedp.ByQuery),
		)
		if err == nil {
			isLoggedIn = true
			log.Println("✅ 检测到已登录状态，跳过登录步骤")
		} else {
			log.Println("ℹ️ 未检测到登录状态 (Cookie 可能已失效)，继续执行登录...")
		}
	} else {
		log.Println("ℹ️ 无有效 Cookie，执行完整登录流程...")
	}

	if !isLoggedIn {
		// 等待页面加载并模拟浏览
		log.Println("⏳ 等待页面加载...")
		err = chromedp.Run(ctx,
			chromedp.WaitReady("body"),
			randomDelay(500*time.Millisecond, 1*time.Second),
			chromedp.ActionFunc(smoothScroll),
			randomDelay(500*time.Millisecond, 1*time.Second),
			// 滚回顶部
			chromedp.Evaluate(`window.scrollTo({top: 0, behavior: 'smooth'})`, nil),
			randomDelay(800*time.Millisecond, 1500*time.Millisecond),
		)
		if err != nil {
			return fmt.Errorf("页面加载失败: %w", err)
		}

		// 点击顶部登录按钮，打开登录弹窗
		log.Println("👆 点击登录按钮...")
		err = chromedp.Run(ctx,
			chromedp.WaitVisible(loginButtonTop, chromedp.ByQuery),
			randomDelay(300*time.Millisecond, 600*time.Millisecond),
			chromedp.Click(loginButtonTop, chromedp.ByQuery),
			randomDelay(1*time.Second, 2*time.Second),
		)
		if err != nil {
			return fmt.Errorf("点击登录按钮失败: %w", err)
		}

		// 点击密码登录选项卡
		log.Println("🔑 切换到密码登录...")
		err = chromedp.Run(ctx,
			chromedp.WaitVisible(passwordTabSelector, chromedp.ByQuery),
			randomDelay(300*time.Millisecond, 800*time.Millisecond),
			chromedp.Click(passwordTabSelector, chromedp.ByQuery),
			randomDelay(500*time.Millisecond, 1*time.Second),
		)
		if err != nil {
			return fmt.Errorf("切换登录方式失败: %w", err)
		}

		// 输入用户名（模拟人类打字速度）
		log.Println("✍️  输入用户名...")
		err = chromedp.Run(ctx,
			chromedp.WaitVisible(usernameSelector, chromedp.ByQuery),
			chromedp.Click(usernameSelector, chromedp.ByQuery),
			randomDelay(200*time.Millisecond, 500*time.Millisecond),
		)
		if err != nil {
			return fmt.Errorf("定位用户名输入框失败: %w", err)
		}

		// 逐字符输入用户名，模拟真实打字
		for _, char := range username {
			if err := chromedp.Run(ctx,
				chromedp.SendKeys(usernameSelector, string(char), chromedp.ByQuery),
				chromedp.Sleep(time.Duration(50+rand.Intn(100))*time.Millisecond),
			); err != nil {
				return fmt.Errorf("输入用户名失败: %w", err)
			}
		}
		chromedp.Sleep(time.Duration(200+rand.Intn(300)) * time.Millisecond).Do(ctx)

		// 输入密码
		log.Println("🔒 输入密码...")
		err = chromedp.Run(ctx,
			chromedp.Click(passwordSelector, chromedp.ByQuery),
			randomDelay(200*time.Millisecond, 400*time.Millisecond),
		)
		if err != nil {
			return fmt.Errorf("定位密码输入框失败: %w", err)
		}

		// 逐字符输入密码
		for _, char := range password {
			if err := chromedp.Run(ctx,
				chromedp.SendKeys(passwordSelector, string(char), chromedp.ByQuery),
				chromedp.Sleep(time.Duration(50+rand.Intn(100))*time.Millisecond),
			); err != nil {
				return fmt.Errorf("输入密码失败: %w", err)
			}
		}
		chromedp.Sleep(time.Duration(300+rand.Intn(500)) * time.Millisecond).Do(ctx)

		// 点击登录按钮
		log.Println("👆 点击登录按钮...")
		err = chromedp.Run(ctx,
			chromedp.Click(loginButtonSelector, chromedp.ByQuery),
			randomDelay(2*time.Second, 3*time.Second),
		)
		if err != nil {
			return fmt.Errorf("点击登录按钮失败: %w", err)
		}

		// 等待登录成功
		log.Println("⏳ 等待登录完成...")
		err = chromedp.Run(ctx,
			chromedp.WaitVisible(postLoginLoadSelector, chromedp.ByQuery),
			randomDelay(1*time.Second, 2*time.Second),
			chromedp.WaitVisible(successSelector, chromedp.ByQuery),
		)
		if err != nil {
			return fmt.Errorf("登录验证失败: %w", err)
		}
	}

	// 登录成功后保存 Cookies
	if err := saveCookies(ctx); err != nil {
		log.Printf("⚠️ 保存 Cookies 失败: %v", err)
	} else {
		log.Println("💾 Cookies 已保存")
	}

	log.Println("✅ 登录成功")
	return nil
}

// CheckForUpdate
func CheckForUpdate(ctx context.Context) ([]storage.Article, error) {
	log.Println("🔍 检查新文章...")

	var htmlContent string
	const articleLinkSelector = `a.articleItem`

	// 模拟真实用户浏览行为
	err := chromedp.Run(ctx,
		chromedp.Navigate(articleURL),
		randomDelay(1*time.Second, 2*time.Second),
		chromedp.WaitReady("body"),
		chromedp.WaitVisible(articleLinkSelector, chromedp.ByQuery),
		randomDelay(500*time.Millisecond, 1*time.Second),
		// 模拟浏览行为
		chromedp.ActionFunc(smoothScroll),
		randomDelay(1*time.Second, 2*time.Second),
		// 滚动回顶部
		chromedp.Evaluate(`window.scrollTo({top: 0, behavior: 'smooth'})`, nil),
		randomDelay(500*time.Millisecond, 1*time.Second),
		chromedp.OuterHTML("html", &htmlContent),
	)

	if err != nil {
		return nil, fmt.Errorf("获取页面内容失败: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return nil, fmt.Errorf("解析HTML失败: %w", err)
	}

	var newArticles []storage.Article
	var processedCount int

	doc.Find(articleLinkSelector).Each(func(i int, s *goquery.Selection) {
		// 跳过置顶文章
		if s.Find("div.articleItem__titles svg").Length() > 0 {
			log.Printf("⏭️ 跳过置顶/官方文章: '%s'", s.Find("div.articleItem__title").Text())
			return
		}

		// only process the first 10 articles
		if processedCount >= 10 {
			return
		}
		processedCount++

		title := strings.TrimSpace(s.Find("div.articleItem__title").Text())
		href, exists := s.Attr("href")
		if !exists {
			return
		}

		author := strings.TrimSpace(s.Find(".articleItem__info-author").Text())
		category := strings.TrimSpace(s.Find(".articleItem__category").Text())
		postedTime := strings.TrimSpace(s.Find(".articleItem__info-time").Text())

		fullURL := "https://bbs.robomaster.com" + href

		// check if the article exists in the database
		exists, err := storage.ArticleExists(fullURL)
		if err != nil {
			log.Printf("⚠️ 检查文章存在性失败: %v", err)
			return
		}

		if !exists {
			newArticle := storage.Article{
				Title:    title,
				URL:      fullURL,
				Author:   author,
				Category: category,
				PostedAt: postedTime,
				Notified: false,
			}

			id, err := storage.SaveArticle(&newArticle)
			if err != nil {
				log.Printf("⚠️ 保存文章失败: %v", err)
				return
			}

			newArticle.ID = id
			newArticles = append(newArticles, newArticle)
		}
	})

	if len(newArticles) > 0 {
		log.Printf("🆕 发现 %d 篇新文章", len(newArticles))
	} else {
		log.Println("✅ 没有发现新文章")
	}

	return newArticles, nil
}
