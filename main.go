package main

import (
	"context"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"os/signal"
	"sort"
	"sync"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/joho/godotenv"
)

var mu sync.Mutex

type Gambler struct {
	UserID      int64
	Gambles     int
	GambleTime  int64
	Username    string
	Wins        int
	AllGambles  int
	NotifyTimer bool
	Notified    bool
}

var (
	botLink     string
	botUserName string
)

func main() {
	godotenv.Load()

	telegramToken := os.Getenv("TELEGRAM_API_TOKEN")

	b, err := bot.New(telegramToken, bot.WithDefaultHandler(handler))
	handleError(err)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	user, err := b.GetMe(ctx)
	handleError(err)

	botUserName = user.Username
	log.Printf("Authorized on account %s", botUserName)
	botLink = fmt.Sprintf("https://t.me/%s", botUserName)

	err = initDB()
	handleError(err)

	go func() {
		for {
			time.Sleep(time.Second * 20)

			mu.Lock()
			gamblers, err := loadGamblerData()
			if err != nil {
				log.Printf("Failed to load gambler data: %v", err)
				mu.Unlock()
				continue
			}
			for _, gambler := range gamblers {
				if gambler.NotifyTimer {
					sinceGamble := time.Since(time.Unix(gambler.GambleTime, 0))
					if sinceGamble.Minutes() > 60 && !gambler.Notified {
						err = notify(ctx, b, gambler)
						if err != nil {
							log.Printf("Can't send message to %s", gambler.Username)
						} else {
							gambler.Notified = true
							saveGamblerData(gambler, 0, "")
						}
					}
				}
			}
			mu.Unlock()
		}
	}()

	b.Start(ctx)
}

func handler(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}

	if update.Message.Text == "/top" || update.Message.Text == "/top@"+botUserName {
		gamblers, err := loadGamblerData()
		handleError(err)
		topText := getTopGamblers(ctx, b, gamblers, update.Message.Chat.ID)
		msg := &bot.SendMessageParams{
			ChatID:              update.Message.Chat.ID,
			Text:                topText,
			ParseMode:           models.ParseModeHTML,
			DisableNotification: true,
		}
		_, err = b.SendMessage(ctx, msg)
		handleError(err)
	}

	if update.Message.Text == "/stats" || update.Message.Text == "/stats@"+botUserName {
		pullStats, err := loadPullStats()
		handleError(err)
		statsText := getDropStats(pullStats)
		msg := &bot.SendMessageParams{
			ChatID:              update.Message.Chat.ID,
			Text:                statsText,
			ParseMode:           models.ParseModeHTML,
			DisableNotification: true,
		}
		_, err = b.SendMessage(ctx, msg)
		handleError(err)
	}

	if update.Message.Text == "/notify" || update.Message.Text == "/notify@"+botUserName {
		mu.Lock()
		gamblers, err := loadGamblerData()
		handleError(err)

		gambler, ok := gamblers[update.Message.From.ID]
		if !ok {
			gambler = &Gambler{
				UserID:      update.Message.From.ID,
				Gambles:     0,
				GambleTime:  time.Now().Unix(),
				Username:    update.Message.From.Username,
				Wins:        0,
				AllGambles:  0,
				NotifyTimer: false,
				Notified:    false,
			}
		}

		gambler.NotifyTimer = !gambler.NotifyTimer

		err = saveGamblerData(gambler, 0, "")
		mu.Unlock()
		handleError(err)

		var msgText string
		if gambler.NotifyTimer {
			msgText = fmt.Sprintf(
				"%s, вы включили уведомления о сбросе таймера гамбы.\n\nНапишите в [ЛС боту](%s) любое сообщение, чтобы разрешить отправку уведомлений.",
				gambler.Username,
				botLink,
			)
		} else {
			msgText = fmt.Sprintf(
				"%s, вы отключили уведомления о сбросе таймера гамбы.",
				gambler.Username,
			)
		}
		msg := &bot.SendMessageParams{
			ChatID:              update.Message.Chat.ID,
			Text:                msgText,
			ParseMode:           models.ParseModeMarkdownV1,
			DisableNotification: true,
		}
		_, err = b.SendMessage(ctx, msg)
		handleError(err)
	}

	if update.Message.Dice == nil || update.Message.ForwardOrigin != nil {
		return
	}

	if update.Message.Dice.Emoji != "🎰" {
		err := sendMessageAndDeleteAfterDelay(ctx, b, update.Message.Chat.ID, update.Message.ID, "ЭТО НЕПРАВИЛЬНАЯ ГАМБА У НАС ТОКА СЛОТИКИ", 5, false)
		handleError(err)
		return
	}

	if update.Message.IsFromOffline {
		mu.Lock()
		gamblers, err := loadGamblerData()
		if err == nil {
			gambler, ok := gamblers[update.Message.From.ID]
			if !ok {
				gambler = &Gambler{
					UserID:      update.Message.From.ID,
					Username:    update.Message.From.Username,
					NotifyTimer: false,
				}
			}
			gambler.Gambles = 3
			gambler.GambleTime = time.Now().Add(time.Hour).Unix()
			saveGamblerData(gambler, 0, "")
		}
		mu.Unlock()

		b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: update.Message.Chat.ID, MessageID: update.Message.ID})
		msgText := fmt.Sprintf("@%s, ТЫ ЗАБАНЕН В ГАМБЕ ЗА ЮЗ ОТЛОЖКИ!!!", update.Message.From.Username)
		b.SendMessage(ctx, &bot.SendMessageParams{ChatID: update.Message.Chat.ID, Text: msgText})
		return
	}

	err := handleGamble(ctx, b, update)
	handleError(err)
}

func handleGamble(ctx context.Context, b *bot.Bot, update *models.Update) (err error) {
	mu.Lock()
	defer mu.Unlock()

	gamblers, err := loadGamblerData()
	if err != nil {
		return
	}
	gambler, ok := gamblers[update.Message.From.ID]
	if !ok {
		gambler = &Gambler{
			UserID:      update.Message.From.ID,
			Gambles:     0,
			GambleTime:  time.Now().Unix(),
			Username:    update.Message.From.Username,
			Wins:        0,
			AllGambles:  0,
			NotifyTimer: false,
			Notified:    false,
		}
	}

	timeSince := time.Since(time.Unix(gambler.GambleTime, 0))
	if timeSince.Minutes() < 60 {
		gambler.Gambles++
		gambler.Notified = false
	} else {
		gambler.Gambles = 1
		gambler.GambleTime = time.Now().Unix()
	}

	if gambler.Gambles > 3 {
		cooldownEnd := time.Unix(gambler.GambleTime, 0).Add(60 * time.Minute)
		remaining := time.Until(cooldownEnd)
		minutes := int(remaining.Minutes())
		seconds := int(remaining.Seconds()) % 60

		msgText := fmt.Sprintf(
			"%s, лимит гамбы превышен!\nПравила гамбы: 3 крутки в час\n\nПопробуйте снова через %d минут %d секунд!\n",
			gambler.Username, minutes, seconds,
		)

		err := sendMessageAndDeleteAfterDelay(ctx, b, update.Message.Chat.ID, update.Message.ID, msgText, 5, false)
		gambler.Gambles = 3
		saveGamblerData(gambler, 0, "")
		return err
	}

	switch update.Message.Dice.Value {
	case 1, 22, 43, 64:
		gambler.Wins++
	}
	gambler.AllGambles++

	err = saveGamblerData(gambler, update.Message.Dice.Value, update.Message.From.Username)
	return err
}

func getTopGamblers(ctx context.Context, b *bot.Bot, gamblers map[int64]*Gambler, chatID int64) string {
	var topGamblers []*Gambler

	for _, gambler := range gamblers {
		chatMember, err := b.GetChatMember(ctx, &bot.GetChatMemberParams{
			ChatID: chatID,
			UserID: gambler.UserID,
		})
		if err != nil {
			continue
		}
		if chatMember.Type == models.ChatMemberTypeOwner ||
			chatMember.Type == models.ChatMemberTypeAdministrator ||
			chatMember.Type == models.ChatMemberTypeMember {
			topGamblers = append(topGamblers, gambler)
		}
	}

	sort.Slice(topGamblers, func(i, j int) bool {
		if topGamblers[i].Wins == 0 && topGamblers[j].Wins == 0 {
			return topGamblers[i].AllGambles < topGamblers[j].AllGambles
		}
		winRateI := float64(topGamblers[i].Wins) / float64(topGamblers[i].AllGambles)
		winRateJ := float64(topGamblers[j].Wins) / float64(topGamblers[j].AllGambles)

		if topGamblers[i].Wins > topGamblers[j].Wins {
			return true
		} else if topGamblers[i].Wins == topGamblers[j].Wins {
			return winRateI > winRateJ
		}
		return false
	})

	sep := "━━━━━━━━━━━━━━━━━━━━━━"
	var text string
	text = "<code>"
	text += sep + "\n"
	text += "     🎰 <b>ТОП ГАМБЫ</b> 🎰\n"
	text += sep + "\n\n"

	maxUserLen := 0
	maxWinsLen := 0
	maxGamblesLen := 0
	for _, g := range topGamblers {
		if l := len(g.Username); l > maxUserLen {
			maxUserLen = l
		}
		if l := len(fmt.Sprintf("%d", g.Wins)); l > maxWinsLen {
			maxWinsLen = l
		}
		if l := len(fmt.Sprintf("%d", g.AllGambles)); l > maxGamblesLen {
			maxGamblesLen = l
		}
	}

	medals := []string{"🥇", "🥈", "🥉"}
	for i, gambler := range topGamblers {
		rank := i + 1
		var prefix string
		padding := maxUserLen
		if rank <= 3 {
			prefix = medals[rank-1] + " "
		} else {
			prefix = fmt.Sprintf("%2d. ", rank)
		}

		winRate := 0.0
		if gambler.AllGambles > 0 {
			winRate = float64(gambler.Wins) / float64(gambler.AllGambles) * 100
		}
		text += fmt.Sprintf("%s%-*s — %*d🏆 / %*d🎰 (%*.1f%%)\n",
			prefix,
			padding, gambler.Username,
			maxWinsLen, gambler.Wins,
			maxGamblesLen, gambler.AllGambles,
			5, winRate)
	}

	text += "</code>"
	return text
}

func getDropStats(pullStats map[string]int) string {
	sep := "━━━━━━━━━━━━━━━━━━━━━━"
	total := pullStats["bar"] + pullStats["grape"] + pullStats["lemon"] + pullStats["seven"]
	text := "<code>"
	text += sep + "\n"
	text += "📊 <b>СТАТИСТИКА</b>\n"
	text += sep + "\n"
	if total > 0 {
		text += fmt.Sprintf("🍾 BAR       %d (%5.1f%%)\n", pullStats["bar"], float64(pullStats["bar"])/float64(total)*100)
		text += fmt.Sprintf("🍇 Виноград  %d (%5.1f%%)\n", pullStats["grape"], float64(pullStats["grape"])/float64(total)*100)
		text += fmt.Sprintf("🍋 Лимон     %d (%5.1f%%)\n", pullStats["lemon"], float64(pullStats["lemon"])/float64(total)*100)
		text += fmt.Sprintf("7️⃣ Семерка   %d (%5.1f%%)\n", pullStats["seven"], float64(pullStats["seven"])/float64(total)*100)
	} else {
		text += fmt.Sprintf("🍾 BAR       %d\n", pullStats["bar"])
		text += fmt.Sprintf("🍇 Виноград  %d\n", pullStats["grape"])
		text += fmt.Sprintf("🍋 Лимон     %d\n", pullStats["lemon"])
		text += fmt.Sprintf("7️⃣ Семерка   %d\n", pullStats["seven"])
	}
	text += "</code>"
	return text
}

func sendMessageAndDeleteAfterDelay(ctx context.Context, b *bot.Bot, chatID int64, messageID int, text string, delay_time float64, isMarkdown bool) error {
	var deleteSticker bot.DeleteMessageParams
	doStickerExist := false

	params := &bot.SendMessageParams{
		ChatID:              chatID,
		Text:                text,
		DisableNotification: true,
	}
	if isMarkdown {
		params.ParseMode = models.ParseModeMarkdownV1
	}

	b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: chatID, MessageID: messageID})

	if rand.IntN(5) == 4 && delay_time == 5 {
		doStickerExist = true
		stickerset, err := b.GetStickerSet(ctx, &bot.GetStickerSetParams{Name: "ChoZaHui_nya_by_fStikBot"})
		if err != nil {
			return err
		}
		var stickerInputFile models.InputFile = &models.InputFileString{Data: stickerset.Stickers[0].FileID}
		stickerMsg := &bot.SendStickerParams{
			ChatID:              chatID,
			Sticker:             stickerInputFile,
			DisableNotification: true,
		}
		sentSticker, err := b.SendSticker(ctx, stickerMsg)
		if err != nil {
			return err
		}
		deleteSticker = bot.DeleteMessageParams{ChatID: chatID, MessageID: sentSticker.ID}
	}

	sentMessage, err := b.SendMessage(ctx, params)
	if err != nil {
		return err
	}

	deleteMessage := bot.DeleteMessageParams{ChatID: chatID, MessageID: sentMessage.ID}

	go func() {
		delay := time.Duration(delay_time) * time.Second
		time.Sleep(delay)
		if doStickerExist {
			b.DeleteMessage(ctx, &deleteSticker)
		}
		b.DeleteMessage(ctx, &deleteMessage)
	}()

	return nil
}

func notify(ctx context.Context, b *bot.Bot, gambler *Gambler) (err error) {
	msg_text := fmt.Sprintf(
		"@%s, время гамбы!\n\nОтключить уведомления можно с помощью команды /notify",
		gambler.Username,
	)

	notification := &bot.SendMessageParams{
		ChatID: gambler.UserID,
		Text:   msg_text,
	}
	_, err = b.SendMessage(ctx, notification)
	return err
}

func handleError(err error) {
	if err != nil {
		log.Println("Handled error!")
		log.Fatal(err)
	}
}
