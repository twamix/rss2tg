package bot

import (
    "fmt"
    "io"
    "log"
    "net/http"
    "os"
    "strconv"
    "strings"
    "time"

    tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
    "rss2tg/internal/config"
    "rss2tg/internal/storage"
    "rss2tg/internal/stats"
)

type MessageHandler func(title, url, group string, pubDate time.Time, matchedKeywords []string) error

type Bot struct {
    api              *tgbotapi.BotAPI
    users            []int64
    channels         []string
    db               *storage.Storage
    config           *config.Config
    configFile       string
    stats            *stats.Stats
    userState        map[int64]string
    messageHandler   MessageHandler
    updateRSSHandler func()
}

func NewBot(token string, users []string, channels []string, db *storage.Storage, config *config.Config, configFile string, stats *stats.Stats) (*Bot, error) {
    api, err := tgbotapi.NewBotAPI(token)
    if err != nil {
        return nil, err
    }

    userIDs := make([]int64, len(users))
    for i, user := range users {
        userID, err := strconv.ParseInt(user, 10, 64)
        if err != nil {
            return nil, fmt.Errorf("无效的用户ID: %s", user)
        }
        userIDs[i] = userID
    }

    return &Bot{
        api:              api,
        users:            userIDs,
        channels:         channels,
        db:               db,
        config:           config,
        configFile:       configFile,
        stats:            stats,
        userState:        make(map[int64]string),
        updateRSSHandler: func() {}, // 初始化为空函数
    }, nil
}

func (b *Bot) SetMessageHandler(handler MessageHandler) {
    b.messageHandler = handler
}

func (b *Bot) SetUpdateRSSHandler(handler func()) {
    b.updateRSSHandler = handler
}

func (b *Bot) Start() {
    log.Println("机器人已启动")
    
    commands := []tgbotapi.BotCommand{
        {Command: "start", Description: "开始/帮助"},
        {Command: "view", Description: "查看类命令"},
        {Command: "users", Description: "用户管理命令"},
        {Command: "edit", Description: "编辑类命令"},
    //    {Command: "stats", Description: "推送统计"},
    }
    
    setMyCommandsConfig := tgbotapi.NewSetMyCommands(commands...)
    _, err := b.api.Request(setMyCommandsConfig)
    if err != nil {
        log.Printf("设置命令失败: %v", err)
    }

    u := tgbotapi.NewUpdate(0)
    u.Timeout = 60

    updates := b.api.GetUpdatesChan(u)

    for update := range updates {
        if update.CallbackQuery != nil {
            // 处理按钮点击
            chatID := update.CallbackQuery.Message.Chat.ID
            userID := update.CallbackQuery.From.ID
            
            switch update.CallbackQuery.Data {
            case "config":
                b.handleConfig(chatID)
            case "list":
                b.handleList(chatID)
            case "stats":
                b.handleStats(chatID)
            case "version":
                b.handleVersion(chatID)
            case "add":
                b.handleAdd(chatID, userID)
            case "edit":
                b.handleEdit(chatID, userID)
            case "delete":
                b.handleDelete(chatID, userID)
            case "add_all":
                b.handleAddAll(chatID, userID)
            case "del_all":
                b.handleDelAll(chatID, userID)
            case "add_user":
                b.handleAddUser(chatID, userID)
            case "del_user":
                b.handleDelUser(chatID, userID)
            case "list_users":
                b.handleListUsers(chatID)
            }
            
            // 回应按钮点击
            callback := tgbotapi.NewCallback(update.CallbackQuery.ID, "")
            if _, err := b.api.Request(callback); err != nil {
                log.Printf("回应按钮点击失败: %v", err)
            }
            
            continue
        }

        if update.Message == nil {
            continue
        }

        userID := update.Message.From.ID
        chatID := update.Message.Chat.ID

        if update.Message.IsCommand() {
            switch update.Message.Command() {
            case "start":
                b.handleStart(chatID)
            case "stats":
                b.handleStats(chatID)
            case "view":
                b.handleView(chatID, userID)
            case "edit":
                b.handleEditCommand(chatID, userID)
            case "config":
                b.handleConfig(chatID)
            case "list":
                b.handleList(chatID)
            case "version":
                b.handleVersion(chatID)
            case "add":
                b.handleAdd(chatID, userID)
            case "delete":
                b.handleDelete(chatID, userID)
            case "users":
                b.handleUsers(chatID, userID)
            default:
                b.sendMessage(chatID, "未知命令，请使用 /start 查看可用命令。")
            }
        } else {
            b.handleUserInput(update.Message)
        }
    }
}

// escapeMarkdownV2Text 转义普通文本中的特殊字符
func escapeMarkdownV2Text(text string) string {
    // 首先转义反斜杠，这样不会影响后续的转义
    text = strings.ReplaceAll(text, "\\", "\\\\")

    // 其他特殊字符的转义
    text = strings.ReplaceAll(text, "_", "\\_")
    text = strings.ReplaceAll(text, "*", "\\*")
    text = strings.ReplaceAll(text, "[", "\\[")
    text = strings.ReplaceAll(text, "]", "\\]")
    text = strings.ReplaceAll(text, "(", "\\(")
    text = strings.ReplaceAll(text, ")", "\\)")
    text = strings.ReplaceAll(text, "~", "\\~")
    text = strings.ReplaceAll(text, "`", "\\`")
    text = strings.ReplaceAll(text, ">", "\\>")
    text = strings.ReplaceAll(text, "#", "\\#")
    text = strings.ReplaceAll(text, "+", "\\+")
    text = strings.ReplaceAll(text, "-", "\\-")
    text = strings.ReplaceAll(text, "=", "\\=")
    text = strings.ReplaceAll(text, "|", "\\|")
    text = strings.ReplaceAll(text, "{", "\\{")
    text = strings.ReplaceAll(text, "}", "\\}")
    text = strings.ReplaceAll(text, ".", "\\.")
    text = strings.ReplaceAll(text, "!", "\\!")

    return text
}

// formatBoldText 格式化加粗文本
func formatBoldText(text string) string {
    if text == "" {
        return "*无*"
    }
    // 先转义特殊字符，再添加加粗标记
    return "*" + escapeMarkdownV2Text(text) + "*"
}

func (b *Bot) SendMessage(title, url, group string, pubDate time.Time, matchedKeywords []string) error {
    chinaLoc, _ := time.LoadLocation("Asia/Shanghai")
    pubDateChina := pubDate.In(chinaLoc)
    
    // 处理标题（加粗）
    formattedTitle := formatBoldText(title)
    
    // 处理URL（转义所有特殊字符）
    formattedURL := escapeMarkdownV2Text(url)
    
    // 处理关键词（加粗并添加#）
    formattedKeywords := make([]string, len(matchedKeywords))
    for i, keyword := range matchedKeywords {
        // 先转义关键词，再添加#和加粗
        escapedKeyword := escapeMarkdownV2Text(keyword)
        formattedKeywords[i] = "\\#*" + escapedKeyword + "*"
    }
    
    // 处理分组（加粗）
    formattedGroup := formatBoldText(group)
    
    // 处理时间（加粗）
    timeStr := pubDateChina.Format("2006-01-02 15:04:05")
    formattedTime := formatBoldText(timeStr)
    
    // 构建消息文本
    text := fmt.Sprintf("%s\n\n🌐 *链接:* %s\n\n🔍 *关键词:* %s\n\n🏷️ *分组:* %s\n\n🕒 *时间:* %s", 
        formattedTitle,
        formattedURL,
        strings.Join(formattedKeywords, " "),
        formattedGroup,
        formattedTime)
    
    log.Printf("发送消息: %s", text)

    // 发送消息
    for _, userID := range b.users {
        msg := tgbotapi.NewMessage(userID, text)
        msg.ParseMode = "MarkdownV2"
        if _, err := b.api.Send(msg); err != nil {
            log.Printf("发送消息给用户 %d 失败: %v", userID, err)
        } else {
            log.Printf("成功发送消息给用户 %d", userID)
            b.stats.IncrementMessageCount()
        }
    }

    for _, channel := range b.channels {
        msg := tgbotapi.NewMessageToChannel(channel, text)
        msg.ParseMode = "MarkdownV2"
        if _, err := b.api.Send(msg); err != nil {
            log.Printf("发送消息到频道 %s 失败: %v", channel, err)
        } else {
            log.Printf("成功发送消息到频道 %s", channel)
            b.stats.IncrementMessageCount()
        }
    }

    return nil
}

func (b *Bot) reloadConfig() error {
    newConfig, err := config.Load(b.configFile)
    if err != nil {
        return err
    }
    b.config = newConfig
    return nil
}

func (b *Bot) handleStart(chatID int64) {
    helpText := "欢迎使用RSS订阅机器人！\n\n" +
        "主要命令：\n" +
        "/start \\- 开始使用机器人并查看帮助信息\n" +
        "/view \\- 查看类命令合集\n" +
        "/users \\- 用户管理命令合集\n" +
        "/edit \\- 编辑类命令合集\n" +
        "/stats \\- 查看推送统计\n\n" +
        "查看类命令（使用 /view 查看）：\n" +
        "/config \\- 查看当前配置\n" +
        "/list \\- 列出所有RSS订阅\n" +
        "/stats \\- 查看推送统计\n" +
        "/version \\- 获取当前版本信息\n\n" +
        "用户管理命令（使用 /users 查看）：\n" +
        "/add\\_user \\- 添加用户\n" +
        "/del\\_user \\- 删除用户\n" +
        "/list\\_users \\- 查看用户列表\n\n" +
        "编辑类命令（使用 /edit 查看）：\n" +
        "/add \\- 添加RSS订阅\n" +
        "/edit \\- 编辑RSS订阅\n" +
        "/delete \\- 删除RSS订阅\n" +
        "/add\\_all \\- 向所有订阅添加关键词\n" +
        "/del\\_all \\- 从所有订阅删除关键词"
    
    // 转义特殊字符，但保持命令格式
    helpText = strings.ReplaceAll(helpText, "!", "\\!")
    helpText = strings.ReplaceAll(helpText, "(", "\\(")
    helpText = strings.ReplaceAll(helpText, ")", "\\)")
    
    msg := tgbotapi.NewMessage(chatID, helpText)
    msg.ParseMode = "MarkdownV2"
    if _, err := b.api.Send(msg); err != nil {
        log.Printf("发送消息失败: %v", err)
    }
}

func (b *Bot) handleView(chatID int64, userID int64) {
    text := "查看类命令列表："
    
    // 创建按钮列表
    keyboard := tgbotapi.NewInlineKeyboardMarkup(
        tgbotapi.NewInlineKeyboardRow(
            tgbotapi.NewInlineKeyboardButtonData("📋 查看当前配置", "config"),
            tgbotapi.NewInlineKeyboardButtonData("📜 列出RSS订阅", "list"),
        ),
        tgbotapi.NewInlineKeyboardRow(
            tgbotapi.NewInlineKeyboardButtonData("📊 查看推送统计", "stats"),
            tgbotapi.NewInlineKeyboardButtonData("ℹ️ 获取当前版本", "version"),
        ),
    )

    msg := tgbotapi.NewMessage(chatID, escapeMarkdownV2Text(text))
    msg.ParseMode = "MarkdownV2"
    msg.ReplyMarkup = keyboard
    if _, err := b.api.Send(msg); err != nil {
        log.Printf("发送消息失败: %v", err)
    }
}

func (b *Bot) handleEditCommand(chatID int64, userID int64) {
    text := "编辑类命令列表："
    
    // 创建按钮列表
    keyboard := tgbotapi.NewInlineKeyboardMarkup(
        tgbotapi.NewInlineKeyboardRow(
            tgbotapi.NewInlineKeyboardButtonData("➕ 添加RSS订阅", "add"),
            tgbotapi.NewInlineKeyboardButtonData("✏️ 编辑RSS订阅", "edit"),
        ),
        tgbotapi.NewInlineKeyboardRow(
            tgbotapi.NewInlineKeyboardButtonData("❌ 删除RSS订阅", "delete"),
            tgbotapi.NewInlineKeyboardButtonData("📝 添加全局关键词", "add_all"),
        ),
        tgbotapi.NewInlineKeyboardRow(
            tgbotapi.NewInlineKeyboardButtonData("🗑️ 删除全局关键词", "del_all"),
        ),
    )

    msg := tgbotapi.NewMessage(chatID, escapeMarkdownV2Text(text))
    msg.ParseMode = "MarkdownV2"
    msg.ReplyMarkup = keyboard
    if _, err := b.api.Send(msg); err != nil {
        log.Printf("发送消息失败: %v", err)
    }
}

func (b *Bot) handleConfig(chatID int64) {
    log.Printf("正在处理查看配置请求，chatID: %d", chatID)
    if err := b.reloadConfig(); err != nil {
        log.Printf("加载配置失败: %v", err)
        b.sendMessage(chatID, fmt.Sprintf("加载配置时出错：%v\n请检查配置文件格式是否正确。", err))
        return
    }
    
    config := b.getConfig()
    if config == "" {
        b.sendMessage(chatID, "当前没有配置信息或配置为空")
        return
    }
    
    b.sendMessage(chatID, config)
    log.Printf("成功发送配置信息到chatID: %d", chatID)
}

func (b *Bot) handleAdd(chatID int64, userID int64) {
    if !b.isAdmin(userID) {
        b.sendMessage(chatID, "您不是系统管理员，无法操作")
        return
    }
    b.userState[userID] = "add_url"
    message := b.listSubscriptions()
    message += "\n请输入要添加的RSS订阅URL（如需添加多个URL，请用英文逗号分隔）："
    
    msg := tgbotapi.NewMessage(chatID, escapeMarkdownV2Text(message))
    msg.ParseMode = "MarkdownV2"
    if _, err := b.api.Send(msg); err != nil {
        log.Printf("发送消息失败: %v", err)
    }
}

func (b *Bot) handleEdit(chatID int64, userID int64) {
    if !b.isAdmin(userID) {
        b.sendMessage(chatID, "您不是系统管理员，无法操作")
        return
    }
    b.userState[userID] = "edit_index"
    message := b.listSubscriptions()
    message += "\n请输入要编辑的RSS订阅编号："
    
    msg := tgbotapi.NewMessage(chatID, escapeMarkdownV2Text(message))
    msg.ParseMode = "MarkdownV2"
    if _, err := b.api.Send(msg); err != nil {
        log.Printf("发送消息失败: %v", err)
    }
}

func (b *Bot) handleDelete(chatID int64, userID int64) {
    if !b.isAdmin(userID) {
        b.sendMessage(chatID, "您不是系统管理员，无法操作")
        return
    }
    b.userState[userID] = "delete"
    message := b.listSubscriptions()
    message += "\n请输入要删除的RSS订阅编号："
    
    msg := tgbotapi.NewMessage(chatID, escapeMarkdownV2Text(message))
    msg.ParseMode = "MarkdownV2"
    if _, err := b.api.Send(msg); err != nil {
        log.Printf("发送消息失败: %v", err)
    }
}

func (b *Bot) handleList(chatID int64) {
    log.Printf("正在处理列表请求，chatID: %d", chatID)
    if err := b.reloadConfig(); err != nil {
        log.Printf("加载配置失败: %v", err)
        b.sendMessage(chatID, fmt.Sprintf("加载配置时出错：%v\n请检查配置文件格式是否正确。", err))
        return
    }
    
    list := b.listSubscriptions()
    if list == "" {
        b.sendMessage(chatID, "当前没有RSS订阅")
        return
    }
    
    b.sendMessage(chatID, list)
    log.Printf("成功发送订阅列表到chatID: %d", chatID)
}

func (b *Bot) handleStats(chatID int64) {
    // 创建新的消息
    msg := tgbotapi.NewMessage(chatID, b.getStats())
    msg.ParseMode = "MarkdownV2"  // 设置解析模式为 MarkdownV2
    
    // 发送消息
    if _, err := b.api.Send(msg); err != nil {
        log.Printf("发送统计信息失败: %v", err)
    }
}

func (b *Bot) handleUserInput(message *tgbotapi.Message) {
    userID := message.From.ID
    chatID := message.Chat.ID
    text := message.Text

    switch b.userState[userID] {
    case "view_command":
        switch text {
        case "1":
            b.handleConfig(chatID)
        case "2":
            b.handleStats(chatID)
        case "3":
            b.handleList(chatID)
        case "4":
            b.handleVersion(chatID)
        default:
            b.sendMessage(chatID, "无效的命令编号，请使用 /view 重新选择。")
        }
        delete(b.userState, userID)
    case "edit_command":
        switch text {
        case "1":
            b.handleAdd(chatID, userID)
        case "2":
            b.handleEdit(chatID, userID)
        case "3":
            b.handleDelete(chatID, userID)
        case "4":
            b.handleAddAll(chatID, userID)
        case "5":
            b.handleDelAll(chatID, userID)
        default:
            b.sendMessage(chatID, "无效的命令编号，请使用 /edit 重新选择。")
            delete(b.userState, userID)
            return
        }
    case "add_url":
        b.userState[userID] = "add_interval"
        urls := strings.Split(text, ",")
        // 清理URL列表
        cleanURLs := make([]string, 0)
        for _, url := range urls {
            url = strings.TrimSpace(url)
            if url != "" {
                cleanURLs = append(cleanURLs, url)
            }
        }
        // 创建新的RSSEntry并添加到配置中，默认允许部分匹配
        b.config.RSS = append(b.config.RSS, config.RSSEntry{
            URLs:           cleanURLs,
            AllowPartMatch: true,  // 默认允许部分匹配
            MatchScope:     config.MatchScopeAll,
        })
        b.sendMessage(chatID, "请输入订阅的更新间隔（秒）：")
    case "add_interval":
        interval, err := strconv.Atoi(text)
        if err != nil {
            b.sendMessage(chatID, "无效的间隔时间，请输入一个整数。")
            return
        }
        lastIndex := len(b.config.RSS) - 1
        if lastIndex >= 0 {
            b.config.RSS[lastIndex].Interval = interval
            b.userState[userID] = "add_keywords"
            b.sendMessage(chatID, "请输入关键词（用空格分隔）：\n1: 保持原有关键词\n2: 不设置关键词（将推送所有新文章）\n或直接输入新的关键词")
        } else {
            b.sendMessage(chatID, "添加订阅失败：找不到要编辑的订阅")
            delete(b.userState, userID)
        }
    case "add_keywords":
        lastIndex := len(b.config.RSS) - 1
        if lastIndex >= 0 {
            switch text {
            case "1":
                // 保持原有关键词，不做任何修改
            case "2":
                // 清空关键词，将推送所有新文章
                b.config.RSS[lastIndex].Keywords = []string{}
            default:
                // 使用新输入的关键词
                keywords := strings.Fields(text)
                b.config.RSS[lastIndex].Keywords = keywords
            }
            b.userState[userID] = "add_exclude_keywords"
            b.sendMessage(chatID, "请输入排除关键词（用空格分隔）：\n1: 不设置排除关键词\n或直接输入要排除的关键词")
        } else {
            b.sendMessage(chatID, "添加订阅失败：找不到要编辑的订阅")
            delete(b.userState, userID)
        }
    case "add_exclude_keywords":
        lastIndex := len(b.config.RSS) - 1
        if lastIndex >= 0 {
            if text == "1" {
                b.config.RSS[lastIndex].ExcludeKeywords = []string{}
            } else {
                b.config.RSS[lastIndex].ExcludeKeywords = strings.Fields(text)
            }
            b.userState[userID] = "add_group"
            b.sendMessage(chatID, "请输入组名：")
        } else {
            b.sendMessage(chatID, "添加订阅失败：找不到要编辑的订阅")
            delete(b.userState, userID)
        }
    case "add_group":
        lastIndex := len(b.config.RSS) - 1
        if lastIndex >= 0 {
            b.config.RSS[lastIndex].Group = text
            b.userState[userID] = "add_part_match"
            b.sendMessage(chatID, "是否允许部分关键词匹配？\n1: 允许（如：关键词\"go\"可以匹配到\"golang\"）\n2: 不允许（仅匹配完整单词）\n请输入选项编号(1或2)：")
        } else {
            b.sendMessage(chatID, "添加订阅失败：找不到要编辑的订阅")
            delete(b.userState, userID)
        }
    case "add_part_match":
        lastIndex := len(b.config.RSS) - 1
        if lastIndex >= 0 {
            switch text {
            case "1":
                b.config.RSS[lastIndex].AllowPartMatch = true
            case "2":
                b.config.RSS[lastIndex].AllowPartMatch = false
            default:
                b.sendMessage(chatID, "无效的选项，请输入1或2：")
                return
            }
            b.userState[userID] = "add_match_scope"
            b.sendMessage(chatID, "请选择关键词匹配范围：\n1: 只匹配标题\n2: 只匹配内容\n3: 匹配标题和内容\n请输入选项编号(1-3)：")
        } else {
            b.sendMessage(chatID, "添加订阅失败：找不到要编辑的订阅")
            delete(b.userState, userID)
        }
    case "add_match_scope":
        lastIndex := len(b.config.RSS) - 1
        if lastIndex >= 0 {
            matchScope, ok := parseMatchScopeOption(text)
            if !ok {
                b.sendMessage(chatID, "无效的选项，请输入1-3。")
                return
            }
            b.config.RSS[lastIndex].MatchScope = matchScope
            delete(b.userState, userID)
            if err := b.config.Save(b.configFile); err != nil {
                b.sendMessage(chatID, "添加订阅成功，但保存配置失败。")
            } else {
                b.sendMessage(chatID, "成功添加RSS订阅。")
                b.updateRSSHandler()
            }
        } else {
            b.sendMessage(chatID, "添加订阅失败：找不到要编辑的订阅")
            delete(b.userState, userID)
        }
    case "edit_index":
        index, err := strconv.Atoi(text)
        if err != nil || index < 1 || index > len(b.config.RSS) {
            b.sendMessage(chatID, "无效的编号。请使用 /edit 重新开始。")
            delete(b.userState, userID)
            return
        }
        b.userState[userID] = fmt.Sprintf("edit_url_%d", index-1)
        b.sendMessage(chatID, fmt.Sprintf("当前URL列表为：\n%s\n请输入新的URL列表（多个URL用英文逗号分隔，如不修改请输入1）：", 
            strings.Join(b.config.RSS[index-1].URLs, "\n")))
    case "delete":
        index, err := strconv.Atoi(text)
        if err != nil || index < 1 || index > len(b.config.RSS) {
            b.sendMessage(chatID, "无效的编号。请使用 /delete 重新开始。")
            delete(b.userState, userID)
            return
        }
        deletedRSS := b.config.RSS[index-1]
        b.config.RSS = append(b.config.RSS[:index-1], b.config.RSS[index:]...)
        if err := b.config.Save(b.configFile); err != nil {
            b.sendMessage(chatID, "删除订阅成功，但保存配置失败。")
        } else {
            b.sendMessage(chatID, fmt.Sprintf("成功删除订阅: %v", deletedRSS.URLs))
            b.updateRSSHandler()
        }
        delete(b.userState, userID)
    case "add_all_keywords":
        keywords := strings.Fields(text)
        if len(keywords) == 0 {
            b.sendMessage(chatID, "请输入至少一个关键词。")
            return
        }
        
        // 向所有订阅添加关键词
        for i := range b.config.RSS {
            existingKeywords := make(map[string]bool)
            for _, k := range b.config.RSS[i].Keywords {
                existingKeywords[strings.ToLower(k)] = true
            }
            
            // 添加新关键词（避免重复）
            for _, newKeyword := range keywords {
                if !existingKeywords[strings.ToLower(newKeyword)] {
                    b.config.RSS[i].Keywords = append(b.config.RSS[i].Keywords, newKeyword)
                }
            }
        }
        
        if err := b.config.Save(b.configFile); err != nil {
            b.sendMessage(chatID, "添加关键词成功，但保存配置失败。")
        } else {
            b.sendMessage(chatID, fmt.Sprintf("成功向所有订阅添加关键词：%v", keywords))
            b.updateRSSHandler()
        }
        delete(b.userState, userID)
        
    case "del_all_keywords":
        keywords := strings.Fields(text)
        if len(keywords) == 0 {
            b.sendMessage(chatID, "请输入至少一个关键词。")
            return
        }
        
        // 从所有订阅中删除关键词
        keywordsToRemove := make(map[string]bool)
        for _, k := range keywords {
            keywordsToRemove[strings.ToLower(k)] = true
        }
        
        for i := range b.config.RSS {
            newKeywords := make([]string, 0)
            for _, k := range b.config.RSS[i].Keywords {
                if !keywordsToRemove[strings.ToLower(k)] {
                    newKeywords = append(newKeywords, k)
                }
            }
            b.config.RSS[i].Keywords = newKeywords
        }
        
        if err := b.config.Save(b.configFile); err != nil {
            b.sendMessage(chatID, "删除关键词成功，但保存配置失败。")
        } else {
            b.sendMessage(chatID, fmt.Sprintf("成功从所有订阅中删除关键词：%v", keywords))
            b.updateRSSHandler()
        }
        delete(b.userState, userID)
    default:
        if strings.HasPrefix(b.userState[userID], "edit_url_") {
            index, _ := strconv.Atoi(strings.TrimPrefix(b.userState[userID], "edit_url_"))
            if text != "1" {
                urls := strings.Split(text, ",")
                // 清理URL列表
                cleanURLs := make([]string, 0)
                for _, url := range urls {
                    url = strings.TrimSpace(url)
                    if url != "" {
                        cleanURLs = append(cleanURLs, url)
                    }
                }
                b.config.RSS[index].URLs = cleanURLs
            }
            b.userState[userID] = fmt.Sprintf("edit_interval_%d", index)
            b.sendMessage(chatID, fmt.Sprintf("当前间隔为：%d秒\n请输入新的间隔时间（秒）如不修改请输入1）：", b.config.RSS[index].Interval))
        } else if strings.HasPrefix(b.userState[userID], "edit_interval_") {
            index, _ := strconv.Atoi(strings.TrimPrefix(b.userState[userID], "edit_interval_"))
            if text != "1" {
                interval, err := strconv.Atoi(text)
                if err != nil {
                    b.sendMessage(chatID, "无效的间隔时间，请输入一个整数。不修改请输入1。")
                    return
                }
                b.config.RSS[index].Interval = interval
            }
            b.userState[userID] = fmt.Sprintf("edit_keywords_%d", index)
            b.sendMessage(chatID, fmt.Sprintf("当前关键词为：%v\n请输入新的关键词（用空格分隔）：\n1: 保持原有关键词\n2: 不设置关键词（将推送所有新文章）\n或直接输入新的关键词", b.config.RSS[index].Keywords))
        } else if strings.HasPrefix(b.userState[userID], "edit_keywords_") {
            index, _ := strconv.Atoi(strings.TrimPrefix(b.userState[userID], "edit_keywords_"))
            switch text {
            case "1":
                // 保持原有关键词，不做任何修改
            case "2":
                // 清空关键词，将推送所有新文章
                b.config.RSS[index].Keywords = []string{}
            default:
                // 使用新输入的关键词
                keywords := strings.Fields(text)
                b.config.RSS[index].Keywords = keywords
            }
            b.userState[userID] = fmt.Sprintf("edit_exclude_keywords_%d", index)
            b.sendMessage(chatID, fmt.Sprintf("当前排除关键词为：%v\n请输入新的排除关键词（用空格分隔）：\n1: 保持原有排除关键词\n2: 不设置排除关键词\n或直接输入新的排除关键词", b.config.RSS[index].ExcludeKeywords))
        } else if strings.HasPrefix(b.userState[userID], "edit_exclude_keywords_") {
            index, _ := strconv.Atoi(strings.TrimPrefix(b.userState[userID], "edit_exclude_keywords_"))
            switch text {
            case "1":
                // 保持原有排除关键词，不做任何修改
            case "2":
                b.config.RSS[index].ExcludeKeywords = []string{}
            default:
                b.config.RSS[index].ExcludeKeywords = strings.Fields(text)
            }
            b.userState[userID] = fmt.Sprintf("edit_group_%d", index)
            b.sendMessage(chatID, fmt.Sprintf("当前组名为：%s\n请输入新的组名（如不修改请输入1）：", b.config.RSS[index].Group))
        } else if strings.HasPrefix(b.userState[userID], "edit_group_") {
            index, _ := strconv.Atoi(strings.TrimPrefix(b.userState[userID], "edit_group_"))
            if text != "1" {
                b.config.RSS[index].Group = text
            }
            b.userState[userID] = fmt.Sprintf("edit_part_match_%d", index)
            b.sendMessage(chatID, fmt.Sprintf("当前部分匹配设置：%v\n是否允许部分关键词匹配？\n1: 允许（如：关键词\"go\"可以匹配到\"golang\"）\n2: 不允许（仅匹配完整单词）\n3: 保持不变\n请输入选项编号(1-3)：", 
                b.config.RSS[index].AllowPartMatch))
        } else if strings.HasPrefix(b.userState[userID], "edit_part_match_") {
            index, _ := strconv.Atoi(strings.TrimPrefix(b.userState[userID], "edit_part_match_"))
            switch text {
            case "1":
                b.config.RSS[index].AllowPartMatch = true
            case "2":
                b.config.RSS[index].AllowPartMatch = false
            case "3":
                // 保持不变
            default:
                b.sendMessage(chatID, "无效的选项，请输入1-3：")
                return
            }
            b.userState[userID] = fmt.Sprintf("edit_match_scope_%d", index)
            b.sendMessage(chatID, fmt.Sprintf("当前关键词匹配范围：%s\n请选择新的匹配范围：\n1: 只匹配标题\n2: 只匹配内容\n3: 匹配标题和内容\n4: 保持不变\n请输入选项编号(1-4)：", b.getMatchScopeStatus(b.config.RSS[index].MatchScope)))
        } else if strings.HasPrefix(b.userState[userID], "edit_match_scope_") {
            index, _ := strconv.Atoi(strings.TrimPrefix(b.userState[userID], "edit_match_scope_"))
            if text != "4" {
                matchScope, ok := parseMatchScopeOption(text)
                if !ok {
                    b.sendMessage(chatID, "无效的选项，请输入1-4。")
                    return
                }
                b.config.RSS[index].MatchScope = matchScope
            }
            delete(b.userState, userID)
            if err := b.config.Save(b.configFile); err != nil {
                b.sendMessage(chatID, "编辑订阅成功，但保存配置失败。")
            } else {
                b.sendMessage(chatID, "成功编辑RSS订阅。")
                b.updateRSSHandler()
            }
        }
    case "add_user":
        userIDs := strings.Fields(text)
        newUsers := make([]int64, 0)
        for _, userIDStr := range userIDs {
            userID, err := strconv.ParseInt(userIDStr, 10, 64)
            if err != nil {
                b.sendMessage(chatID, fmt.Sprintf("无效的用户ID: %s", userIDStr))
                continue
            }
            if !contains(b.users, userID) {
                newUsers = append(newUsers, userID)
            }
        }
        
        if len(newUsers) > 0 {
            b.users = append(b.users, newUsers...)
            // 更新配置文件
            b.config.Telegram.Users = make([]string, len(b.users))
            for i, uid := range b.users {
                b.config.Telegram.Users[i] = strconv.FormatInt(uid, 10)
            }
            if err := b.config.Save(b.configFile); err != nil {
                b.sendMessage(chatID, "添加用户成功，但保存配置失败")
            } else {
                b.sendMessage(chatID, fmt.Sprintf("成功添加 %d 个用户", len(newUsers)))
            }
        } else {
            b.sendMessage(chatID, "未添加任何新用户")
        }
        delete(b.userState, userID)
    case "del_user":
        index, err := strconv.Atoi(text)
        if err != nil || index < 1 || index > len(b.users) {
            b.sendMessage(chatID, "无效的用户编号")
            delete(b.userState, userID)
            return
        }
        
        deletedUser := b.users[index-1]
        b.users = append(b.users[:index-1], b.users[index:]...)
        
        // 更新配置文件
        b.config.Telegram.Users = make([]string, len(b.users))
        for i, uid := range b.users {
            b.config.Telegram.Users[i] = strconv.FormatInt(uid, 10)
        }
        
        if err := b.config.Save(b.configFile); err != nil {
            b.sendMessage(chatID, "删除用户成功，但保存配置失败")
        } else {
            b.sendMessage(chatID, fmt.Sprintf("成功删除用户: %d", deletedUser))
        }
        delete(b.userState, userID)
    }
}

func (b *Bot) getConfig() string {
    result := "当前配置信息：\n"
    result += fmt.Sprintf("用户: %v\n", b.users)
    result += fmt.Sprintf("频道: %v\n", b.channels)
    result += "RSS订阅:\n"
    for i, rss := range b.config.RSS {
        result += fmt.Sprintf("%d. URLs:\n", i+1)
        for j, url := range rss.URLs {
            result += fmt.Sprintf("   %d) %s\n", j+1, url)
        }
        keywords := strings.Join(rss.Keywords, ", ")
        excludeKeywords := strings.Join(rss.ExcludeKeywords, ", ")
        result += fmt.Sprintf("   间隔: %d秒\n   关键词: %s\n   排除关键词: %s\n   组名: %s\n   部分匹配: %s\n   匹配范围: %s\n",
            rss.Interval,
            keywords,
            excludeKeywords,
            rss.Group,
            b.getPartMatchStatus(rss.AllowPartMatch),
            b.getMatchScopeStatus(rss.MatchScope))
    }
    return result
}

func (b *Bot) listSubscriptions() string {
    result := "当前RSS订阅列表:\n"
    for i, rss := range b.config.RSS {
        result += fmt.Sprintf("%d. URLs:\n", i+1)
        for j, url := range rss.URLs {
            result += fmt.Sprintf("   %d) %s\n", j+1, url)
        }
        keywords := strings.Join(rss.Keywords, ", ")
        excludeKeywords := strings.Join(rss.ExcludeKeywords, ", ")
        result += fmt.Sprintf("   间隔: %d秒\n   关键词: %s\n   排除关键词: %s\n   组名: %s\n   部分匹配: %s\n   匹配范围: %s\n",
            rss.Interval,
            keywords,
            excludeKeywords,
            rss.Group,
            b.getPartMatchStatus(rss.AllowPartMatch),
            b.getMatchScopeStatus(rss.MatchScope))
    }
    return result
}
func (b *Bot) getStats() string {
    dailyCount, weeklyCount, totalCount := b.stats.GetMessageCounts()
    return fmt.Sprintf("推送统计:\n📊 今日推送: %s\n📈 本周推送: %s\n📋 总计推送: %s", 
        formatBoldText(strconv.Itoa(dailyCount)),
        formatBoldText(strconv.Itoa(weeklyCount)),
        formatBoldText(strconv.Itoa(totalCount)))
}

func (b *Bot) UpdateConfig(cfg *config.Config) {
    b.config = cfg
}

func (b *Bot) handleVersion(chatID int64) {
    // 获取当前版本
    currentVersion, err := b.getCurrentVersion()
    if err != nil {
        b.sendMessage(chatID, fmt.Sprintf("获取当前版本失败：%v", err))
        return
    }

    // 获取最新版本
    latestVersion, err := b.getLatestVersion()
    if err != nil {
        b.sendMessage(chatID, fmt.Sprintf("获取最新版本失败：%v", err))
        return
    }

    // 构建版本信息消息
    message := fmt.Sprintf("🤖 *RSS2TG 机器人*\n\n"+
        "当前版本：%s\n"+
        "最新版本：%s\n\n"+
        "©️ 版权所有 2025 drfyup\n"+
        "🌐 官方网站：[GitHub](%s)\n"+
        "📖 官方文档：`%s` \\(点击复制\\)",
        escapeMarkdownV2Text(currentVersion),
        escapeMarkdownV2Text(latestVersion),
        "https://github\\.com/3377/rss2tg",
        "https://3377\\.github\\.io/rss2tg")  // 转义点号

    msg := tgbotapi.NewMessage(chatID, message)
    msg.ParseMode = "MarkdownV2"
    if _, err := b.api.Send(msg); err != nil {
        log.Printf("发送版本信息失败: %v", err)
    }
}

func (b *Bot) getCurrentVersion() (string, error) {
    versionFile := "/app/config/version"
    content, err := os.ReadFile(versionFile)
    if err != nil {
        return "", fmt.Errorf("读取版本文件失败: %v", err)
    }
    return strings.TrimSpace(string(content)), nil
}

func (b *Bot) getLatestVersion() (string, error) {
    // 直接从远程获取最新版本
    resp, err := http.Get("https://raw.githubusercontent.com/3377/rss2tg/refs/heads/main/version")
    if err != nil {
        return "", fmt.Errorf("无法获取最新版本: %v", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return "", fmt.Errorf("获取最新版本失败，状态码: %d", resp.StatusCode)
    }

    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return "", fmt.Errorf("读取最新版本内容失败: %v", err)
    }

    return strings.TrimSpace(string(body)), nil
}

func (b *Bot) handleAddAll(chatID int64, userID int64) {
    if !b.isAdmin(userID) {
        b.sendMessage(chatID, "您不是系统管理员，无法操作")
        return
    }
    b.userState[userID] = "add_all_keywords"
    b.sendMessage(chatID, "请输入要添加到所有订阅的关键词（用空格分隔）：")
}

func (b *Bot) handleDelAll(chatID int64, userID int64) {
    if !b.isAdmin(userID) {
        b.sendMessage(chatID, "您不是系统管理员，无法操作")
        return
    }
    b.userState[userID] = "del_all_keywords"
    b.sendMessage(chatID, "请输入要从所有订阅中删除的关键词（用空格分隔）：")
}

// sendMessage 发送普通消息
func (b *Bot) sendMessage(chatID int64, text string) {
    // 转义特殊字符
    text = escapeMarkdownV2Text(text)
    
    msg := tgbotapi.NewMessage(chatID, text)
    msg.ParseMode = "MarkdownV2"
    if _, err := b.api.Send(msg); err != nil {
        log.Printf("发送消息失败: %v", err)
    }
}

// 辅助函数：获取部分匹配状态的描述
func (b *Bot) getPartMatchStatus(allowPartMatch bool) string {
    if allowPartMatch {
        return "允许"
    }
    return "禁用"
}

func parseMatchScopeOption(option string) (string, bool) {
    switch strings.TrimSpace(option) {
    case "1":
        return config.MatchScopeTitle, true
    case "2":
        return config.MatchScopeContent, true
    case "3":
        return config.MatchScopeAll, true
    default:
        return "", false
    }
}

func (b *Bot) getMatchScopeStatus(matchScope string) string {
    switch strings.ToLower(strings.TrimSpace(matchScope)) {
    case config.MatchScopeTitle:
        return "只匹配标题"
    case config.MatchScopeContent:
        return "只匹配内容"
    case config.MatchScopeAll, "":
        return "匹配标题和内容"
    default:
        return matchScope
    }
}

func (b *Bot) handleUsers(chatID int64, userID int64) {
    text := "用户管理命令列表："
    
    // 创建按钮列表
    keyboard := tgbotapi.NewInlineKeyboardMarkup(
        tgbotapi.NewInlineKeyboardRow(
            tgbotapi.NewInlineKeyboardButtonData("➕ 添加用户", "add_user"),
            tgbotapi.NewInlineKeyboardButtonData("❌ 删除用户", "del_user"),
        ),
        tgbotapi.NewInlineKeyboardRow(
            tgbotapi.NewInlineKeyboardButtonData("📋 查看用户列表", "list_users"),
        ),
    )

    msg := tgbotapi.NewMessage(chatID, escapeMarkdownV2Text(text))
    msg.ParseMode = "MarkdownV2"
    msg.ReplyMarkup = keyboard
    if _, err := b.api.Send(msg); err != nil {
        log.Printf("发送消息失败: %v", err)
    }
}

func (b *Bot) handleAddUser(chatID int64, userID int64) {
    if !b.isAdmin(userID) {
        b.sendMessage(chatID, "您不是系统管理员，无法操作")
        return
    }
    b.userState[userID] = "add_user"
    b.sendMessage(chatID, "请输入要添加的用户ID（多个用户ID请用空格分隔）：")
}

func (b *Bot) handleDelUser(chatID int64, userID int64) {
    if !b.isAdmin(userID) {
        b.sendMessage(chatID, "您不是系统管理员，无法操作")
        return
    }
    b.userState[userID] = "del_user"
    userList := "当前用户列表:\n"
    for i, uid := range b.users {
        userList += fmt.Sprintf("%d. %d\n", i+1, uid)
    }
    userList += "\n请输入要删除的用户编号："
    b.sendMessage(chatID, userList)
}

func (b *Bot) handleListUsers(chatID int64) {
    userList := "当前用户列表:\n"
    for i, uid := range b.users {
        userList += fmt.Sprintf("%d. %d\n", i+1, uid)
    }
    b.sendMessage(chatID, userList)
}

// 添加管理员检查函数
func (b *Bot) isAdmin(userID int64) bool {
    // 如果没有配置管理员，则所有用户都是管理员
    if len(b.config.Telegram.AdminUsers) == 0 {
        return contains(b.users, userID)
    }
    // 检查用户是否在管理员列表中
    for _, adminStr := range b.config.Telegram.AdminUsers {
        adminID, err := strconv.ParseInt(adminStr, 10, 64)
        if err == nil && adminID == userID {
            return true
        }
    }
    return false
}

// 添加 contains 辅助函数
func contains(slice []int64, item int64) bool {
    for _, v := range slice {
        if v == item {
            return true
        }
    }
    return false
}
