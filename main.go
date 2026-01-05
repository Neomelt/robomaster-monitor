package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/chromedp/chromedp"
	"github.com/fsnotify/fsnotify"

	"robomaster-monitor/internal/crawler"
	"robomaster-monitor/internal/notifier"
	"robomaster-monitor/internal/storage"
)

const (
	// ==========================================
	// ===> 在这里修改频率 (Change the frequency here) <===
	// ==========================================================
	checkInterval = 1 * time.Minute // 配合Cookie持久化，可适当提高频率。建议不低于1分钟。
	configFile    = "config/param.toml"
	dbFile        = "data/articles.db"
	logFile       = "logs/app.log"
	// ==========================================================
)

type Config struct {
	DJI struct {
		Username string `toml:"username"`
		Password string `toml:"password"`
	} `toml:"dji"`

	Feishu struct {
		WebhookURL      string `toml:"webhook_url"`
		ErrorWebhookURL string `toml:"error_webhook_url"`
	} `toml:"feishu"`

	Browser struct {
		Headless           bool   `toml:"headless"`
		NoSandbox          bool   `toml:"no_sandbox"`
		DisableGPU         bool   `toml:"disable_gpu"`
		DisableDevShmUsage bool   `toml:"disable_dev_shm_usage"`
		UserAgent          string `toml:"user_agent"`
	} `toml:"browser"`
}

var config Config

// loadConfig
func loadConfig(path string) {
	if _, err := toml.DecodeFile(path, &config); err != nil {
		log.Fatalf("❌  读取配置文件失败: %v", err)
	}
	log.Println("✅  配置文件加载成功")
}

// watchConfig
func watchConfig(path string) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		defer watcher.Close()
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Write == fsnotify.Write {
					log.Println("🔄  检测到配置文件修改，重新加载配置...")
					loadConfig(path)
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("❌  配置文件监听错误:", err)
			}
		}
	}()

	err = watcher.Add(path)
	if err != nil {
		log.Fatal("添加配置文件监听失败:", err)
	}
}

// runPipeline
func runPipeline() {
	// 异常捕获
	defer func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("panic recovered: %v", r)
			log.Printf("🔥 程序发生严重错误: %v", err)
			notifyError(err)
		}
	}()

	username := config.DJI.Username
	password := config.DJI.Password
	webhookURL := config.Feishu.WebhookURL

	if username == "" || password == "" {
		log.Println("⚠️ 缺少用户名或密码，跳过执行")
		return
	}

	// create context
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", config.Browser.Headless),
		chromedp.Flag("no-sandbox", config.Browser.NoSandbox),
		chromedp.Flag("disable-gpu", config.Browser.DisableGPU),
		chromedp.Flag("disable-dev-shm-usage", config.Browser.DisableDevShmUsage),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("disable-web-security", false),
		chromedp.Flag("disable-features", "IsolateOrigins,site-per-process"),
		chromedp.WindowSize(1920, 1080),
		chromedp.UserAgent(config.Browser.UserAgent),
	)

	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()

	ctx, cancel := chromedp.NewContext(allocCtx, chromedp.WithLogf(log.Printf))
	defer cancel()

	// set timeout (增加超时时间以适应更慢的操作)
	ctx, cancel = context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// login
	log.Println("🔐 开始登录...")
	err := crawler.Login(ctx, username, password)
	if err != nil {
		log.Printf("❌ 登录失败: %v", err)
		notifyError(fmt.Errorf("登录失败: %w", err))
		return
	}
	log.Println("✅ 登录成功")

	// 登录成功后短暂等待，模拟人类行为
	time.Sleep(2 * time.Second)

	// check for updates
	newArticles, err := crawler.CheckForUpdate(ctx)
	if err != nil {
		log.Printf("❌ 检查更新失败: %v", err)
		notifyError(fmt.Errorf("检查更新失败: %w", err))
		return
	}

	// send notifications
	if len(newArticles) > 0 {
		log.Printf("🔔 准备通知 %d 篇新文章...", len(newArticles))

		for _, article := range newArticles {
			if webhookURL != "" {

				log.Printf("📤 正在发送通知: %s", article.Title)
				if err := notifier.Send(webhookURL, article.Title, article.URL); err != nil {
					log.Printf("❌ 飞书通知失败: %v", err)
				} else {
					log.Println("✅ 飞书通知发送成功")
					// update article as notified
					if err := storage.MarkAsNotified(article.ID); err != nil {
						log.Printf("⚠️ 更新通知状态失败: %v", err)
					}
				}

				// avoid hitting rate limits (增加随机延迟，更像人类)
				time.Sleep(time.Duration(1000+rand.Intn(2000)) * time.Millisecond)
			}
		}
	}
}

// setupLogging 配置日志输出到文件和控制台
func setupLogging() {
	// 创建日志目录
	if err := os.MkdirAll("logs", 0755); err != nil {
		log.Fatalf("❌ 创建日志目录失败: %v", err)
	}

	// 打开日志文件
	f, err := os.OpenFile(logFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("❌ 打开日志文件失败: %v", err)
	}

	// 设置多重输出
	mw := io.MultiWriter(os.Stdout, f)
	log.SetOutput(mw)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
}

// notifyError 发送错误通知
func notifyError(err error) {
	if config.Feishu.ErrorWebhookURL == "" {
		return
	}
	go func() {
		if sendErr := notifier.SendError(config.Feishu.ErrorWebhookURL, err.Error()); sendErr != nil {
			log.Printf("❌ 发送错误通知失败: %v", sendErr)
		}
	}()
}

func main() {
	// 配置日志
	setupLogging()

	// 创建数据目录
	if err := os.MkdirAll("data", 0755); err != nil {
		log.Fatalf("❌ 创建数据目录失败: %v", err)
	}

	// 初始化数据库
	if err := storage.InitDB(dbFile); err != nil {
		log.Fatalf("❌ 初始化数据库失败: %v", err)
	}
	defer storage.Close()

	// 加载配置
	loadConfig(configFile)

	// 启动配置文件热加载监听
	watchConfig(configFile)

	log.Println("🚀 RoboMaster Monitor 启动成功")

	// 立即运行一次
	runPipeline()

	// 定时运行
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		log.Printf("⏱️ %v 后进行下一次检查", checkInterval)
		<-ticker.C
		runPipeline()
	}
}
