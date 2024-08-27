package main

import (
	"bufio"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
)

// Gambler represents a user who has gambled
//
// Gambles - the number of times the user has gambled (resets every 60 minutes)
// GambleTime - the time when the user last gambled
// Username - the username of the user
// Wins - the number of times the user has won
// AllGambles - the total number of gambles the user has made
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

func main() {
	// Load .env file
	err := godotenv.Load()
	handleError(err)

	// Create bot
	telegramToken := os.Getenv("TELEGRAM_API_TOKEN")
	adminUsername := os.Getenv("ADMIN_USERNAME")
	bot, err := tgbotapi.NewBotAPI(telegramToken)
	handleError(err)

	log.Printf("Authorized on account %s", bot.Self.UserName)

	go func() {
		for {
			time.Sleep(time.Second * 20)
			gamblers, err := loadGamblerData()
			handleError(err)
			for _, gambler := range gamblers {
				if gambler.NotifyTimer {
					sinceGamble := time.Since(time.Unix(gambler.GambleTime, 0))
					if sinceGamble.Minutes() > 60 && !gambler.Notified {
						err = notify(bot, gambler)
						if err != nil {
							log.Printf("Can't send message to %s", gambler.Username)
						} else {
							gambler.Notified = true
						}
						saveGamblerData(gamblers, 0, "")
					}
				}
			}

		}
	}()

	// Create update channel
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)
	// Start listening for updates
	for update := range updates {
		// Skip if the update doesn't contain a message
		if update.Message == nil {
			continue
		}

		// Handle /top command
		if update.Message.Text == "/top" {
			gamblers, err := loadGamblerData()
			handleError(err)
			err = handleTopCommand(bot, update.Message.Chat.ID, update.Message.MessageID, gamblers)
			handleError(err)
		}

		if update.Message.Text == "/pulls" {
			if update.Message.From.UserName == adminUsername {
				pullStats, err := loadPullStats()
				handleError(err)
				counts := make(map[string]int)
				for _, entry := range pullStats {
					counts[entry]++
				}
				var entries []string
				for entry, count := range counts {
					entries = append(entries, fmt.Sprintf("%s: %d", entry, count))
				}
				err = sendMessageAndDeleteAfterDelay(bot, update.Message.Chat.ID, update.Message.MessageID, strings.Join(entries, "\n"), 20, false)
			} else {
				err = sendMessageAndDeleteAfterDelay(bot, update.Message.Chat.ID, update.Message.MessageID, "Ты не величайший админ", 2.5, false)
				handleError(err)
			}
		}

		// Handle /notify command
		if update.Message.Text == "/notify" {
			gamblers, err := loadGamblerData()
			handleError(err)

			// Find the user's gambler
			gambler, ok := gamblers[update.Message.From.ID]
			if !ok {
				gambler = &Gambler{
					UserID:      update.Message.From.ID,
					Gambles:     0,
					GambleTime:  time.Now().Unix(),
					Username:    update.Message.From.UserName,
					Wins:        0,
					AllGambles:  0,
					NotifyTimer: false,
					Notified:    false,
				}
				gamblers[update.Message.From.ID] = gambler
			}

			// Toggle the notify timer
			gambler.NotifyTimer = !gambler.NotifyTimer

			// Save the gambler
			err = saveGamblerData(gamblers, 0, "")
			handleError(err)

			// Send a message with a confirmation
			var msg_text string
			if gambler.NotifyTimer {
				msg_text = fmt.Sprintf(
					"%s, вы включили уведомления о сбросе таймера гамбы.\n\nНапишите в [ЛС боту](https://t.me/gamba_bunker_bot) любое сообщение, чтобы разрешить отправку уведомлений.",
					gambler.Username,
				)
			} else {
				msg_text = fmt.Sprintf(
					"%s, вы отключили уведомления о сбросе таймера гамбы.",
					gambler.Username,
				)
			}
			err = sendMessageAndDeleteAfterDelay(bot, update.Message.Chat.ID, update.Message.MessageID, msg_text, 20, true)
			handleError(err)
		}

		// Skip if the message is not a dice or is a forwarded message
		if update.Message.Dice == nil || update.Message.ForwardFrom != nil {
			continue
		}

		// Skip if the dice emoji is not 🎰
		if update.Message.Dice.Emoji != "🎰" {
			continue
		}

		// Handle gambles
		err = handleGamble(bot, update)
		handleError(err)
	}
}

func handleTopCommand(bot *tgbotapi.BotAPI, chatID int64, msgID int, gamblers map[int64]*Gambler) error {
	// Generate the top text
	topText := getTopGamblers(gamblers, bot, chatID)

	// Send the top text with a delay of 60 seconds and handle any errors
	err := sendMessageAndDeleteAfterDelay(bot, chatID, msgID, topText, 60, false)
	return err
}

func handleGamble(bot *tgbotapi.BotAPI, update tgbotapi.Update) (err error) {
	// Get or create the gambler
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
			Username:    update.Message.From.UserName,
			Wins:        0,
			AllGambles:  0,
			NotifyTimer: false,
			Notified:    false,
		}
		gamblers[update.Message.From.ID] = gambler
	}

	// Check if the gambling limit has been reached
	timeSince := time.Since(time.Unix(gambler.GambleTime, 0))
	if timeSince.Minutes() < 60 {
		gambler.Gambles++ // Increment the number of gambles
		gambler.Notified = false
	} else {
		gambler.Gambles = 1 // Reset the number of gambles
		gambler.GambleTime = time.Now().Unix()
	}

	// Check if the gambling limit has been exceeded
	if gambler.Gambles > 3 {
		// Calculate the remaining time
		minutes := int(60 - timeSince.Minutes())
		seconds := 60 - (int(timeSince.Seconds()) % 60)

		// Send a message with the remaining time
		msg_text := fmt.Sprintf(
			"%s, лимит гамбы превышен!\nПравила гамбы: 3 крутки в час\n\nПопробуйте снова через %d минут %d секунд!\n",
			gambler.Username, minutes, seconds,
		)

		err := sendMessageAndDeleteAfterDelay(bot, update.Message.Chat.ID, update.Message.MessageID, msg_text, 2.5, false)
		gambler.Gambles = 3 // Reset the number of gambles
		return err
	} else {
		// Check the dice value and update the gambler's stats
		switch update.Message.Dice.Value {
		case 1, 22, 43, 64:
			gambler.Wins++
		}
		gambler.AllGambles++

		// Save the gambler's stats
		err := saveGamblerData(gamblers, update.Message.Dice.Value, update.Message.From.UserName)
		return err
	}
}
func saveGamblerData(gamblers map[int64]*Gambler, gambaPull int, gambaPullUsername string) error {
	// Define the filename for the data file.
	const filename = "gamba.txt"

	// Create the file for writing.
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	for UserID, gambler := range gamblers {
		// Format the line to be written to the file.
		line := fmt.Sprintf("%d %d %d %s %d %d %t %t\n",
			UserID,
			gambler.Gambles,
			gambler.GambleTime,
			gambler.Username,
			gambler.Wins,
			gambler.AllGambles,
			gambler.NotifyTimer,
			gambler.Notified,
		)

		// Write the line to the file.
		_, err = file.WriteString(line)
		if err != nil {
			return err
		}

	}
	if gambaPull > 0 && gambaPullUsername != "" {
		const filename2 = "gamba_pulls.txt"
		file2, err := os.OpenFile(filename2, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
		if err != nil {
			return err
		}
		defer file2.Close()
		_, err = file2.WriteString(gambaPullUsername + " " + strconv.Itoa(gambaPull) + "\n")
		if err != nil {
			return err
		}
	}

	// Return no error if the data was successfully saved.
	return nil
}
func loadGamblerData() (map[int64]*Gambler, error) {
	const filename = "gamba.txt"

	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	gamblers := make(map[int64]*Gambler)
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), " ") // Split the line into fields.

		// Parse each field into the corresponding type.
		UserID, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			return nil, err
		}

		Gambles, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, err
		}

		GambleTime, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			return nil, err
		}

		Wins, err := strconv.Atoi(fields[4])
		if err != nil {
			return nil, err
		}

		AllGambles, err := strconv.Atoi(fields[5])
		if err != nil {
			return nil, err
		}

		NotifyTimer, err := strconv.ParseBool(fields[6])
		if err != nil {
			return nil, err
		}

		Notified, err := strconv.ParseBool(fields[7])
		if err != nil {
			return nil, err
		}
		// Create a new gambler and add it to the map.
		gambler := &Gambler{
			UserID:      UserID,
			Gambles:     Gambles,
			GambleTime:  GambleTime,
			Username:    fields[3],
			Wins:        Wins,
			AllGambles:  AllGambles,
			NotifyTimer: NotifyTimer,
			Notified:    Notified,
		}
		gamblers[UserID] = gambler
	}

	return gamblers, nil
}
func getTopGamblers(gamblers map[int64]*Gambler, bot *tgbotapi.BotAPI, chatID int64) string {
	var topGamblers []*Gambler

	// Iterate over the gamblers map and filter out gamblers who are not in this chat.
	for _, gambler := range gamblers {
		chatMember, _ := bot.GetChatMember(tgbotapi.GetChatMemberConfig{
			ChatConfigWithUser: tgbotapi.ChatConfigWithUser{
				ChatID: chatID,
				UserID: gambler.UserID,
			},
		})
		if chatMember.Status == "administrator" || chatMember.Status == "creator" || chatMember.Status == "member" {
			topGamblers = append(topGamblers, gambler)
		}
	}

	// Sort the top gamblers based on their win count and winrate.
	sort.Slice(topGamblers, func(i, j int) bool {
		if topGamblers[i].Wins == 0 && topGamblers[j].Wins == 0 {
			return topGamblers[i].AllGambles < topGamblers[j].AllGambles
		}
		winRateI := float64(topGamblers[i].Wins) / float64(topGamblers[i].AllGambles)
		winRateJ := float64(topGamblers[j].Wins) / float64(topGamblers[j].AllGambles)

		// If the win count for any of the gamblers is greater than the other, they are greater.
		// If the win count is equal, compare the win rate, gambler with higher winrate is greater.
		if topGamblers[i].Wins > topGamblers[j].Wins {
			return true
		} else if topGamblers[i].Wins == topGamblers[j].Wins {
			return winRateI > winRateJ
		}
		return false
	})

	var topGamblersText = "Правила гамбы: 3 крутки в час\n\n🎰 ТОП ГАМБЫ\n\n"
	for _, gambler := range topGamblers {
		topGamblersText += fmt.Sprintf("%s - %d побед - %d круток\n", gambler.Username, gambler.Wins, gambler.AllGambles)
	}

	return topGamblersText
}
func sendMessageAndDeleteAfterDelay(bot *tgbotapi.BotAPI, chatID int64, messageID int, text string, delay_time float64, isMarkdown bool) error {
	// Create the message to send
	var deleteSticker tgbotapi.DeleteMessageConfig
	doStickerExist := false
	message := tgbotapi.NewMessage(chatID, text)
	message.DisableNotification = true
	if isMarkdown {
		message.ParseMode = "Markdown"
	}

	// Delete the original message
	bot.Send(tgbotapi.NewDeleteMessage(chatID, messageID))

	if rand.IntN(20) == 4 && delay_time == 2.5 {
		doStickerExist = true
		stickerset, err := bot.GetStickerSet(tgbotapi.GetStickerSetConfig{Name: "ch4ogpack_by_fStikBot"})
		if err != nil {
			return err
		}
		stickerMsg := tgbotapi.NewSticker(chatID, tgbotapi.FileID(stickerset.Stickers[0].FileID))
		stickerMsg.DisableNotification = true
		sentSticker, err := bot.Send(stickerMsg)
		if err != nil {
			return err
		}
		deleteSticker = tgbotapi.NewDeleteMessage(chatID, sentSticker.MessageID)
	}
	// Send the message and get the sent message
	sentMessage, err := bot.Send(message)
	if err != nil {
		return err
	}

	// Create a message to delete the sent message
	deleteMessage := tgbotapi.NewDeleteMessage(chatID, sentMessage.MessageID)

	// Start a goroutine to delete the sent message after the specified delay
	go func() {
		delay := time.Duration(delay_time) * time.Second
		time.Sleep(delay)
		if doStickerExist {
			bot.Send(deleteSticker)
		}
		bot.Send(deleteMessage)
	}()

	return nil
}
func notify(bot *tgbotapi.BotAPI, gambler *Gambler) (err error) {
	msg_text := fmt.Sprintf(
		"@%s, время гамбы!\n\nОтключить уведомления можно с помощью команды /notify",
		gambler.Username,
	)

	notification := tgbotapi.NewMessage(gambler.UserID, msg_text)
	_, err = bot.Send(notification)
	return err
}
func loadPullStats() (pullStats []string, err error) {
	const filename = "gamba_pulls.txt"
	pullStats = make([]string, 0)
	var slotMachineValue = map[int][3]string{
		1:  {"bar", "bar", "bar"},
		2:  {"grape", "bar", "bar"},
		3:  {"lemon", "bar", "bar"},
		4:  {"seven", "bar", "bar"},
		5:  {"bar", "grape", "bar"},
		6:  {"grape", "grape", "bar"},
		7:  {"lemon", "grape", "bar"},
		8:  {"seven", "grape", "bar"},
		9:  {"bar", "lemon", "bar"},
		10: {"grape", "lemon", "bar"},
		11: {"lemon", "lemon", "bar"},
		12: {"seven", "lemon", "bar"},
		13: {"bar", "seven", "bar"},
		14: {"grape", "seven", "bar"},
		15: {"lemon", "seven", "bar"},
		16: {"seven", "seven", "bar"},
		17: {"bar", "bar", "grape"},
		18: {"grape", "bar", "grape"},
		19: {"lemon", "bar", "grape"},
		20: {"seven", "bar", "grape"},
		21: {"bar", "grape", "grape"},
		22: {"grape", "grape", "grape"},
		23: {"lemon", "grape", "grape"},
		24: {"seven", "grape", "grape"},
		25: {"bar", "lemon", "grape"},
		26: {"grape", "lemon", "grape"},
		27: {"lemon", "lemon", "grape"},
		28: {"seven", "lemon", "grape"},
		29: {"bar", "seven", "grape"},
		30: {"grape", "seven", "grape"},
		31: {"lemon", "seven", "grape"},
		32: {"seven", "seven", "grape"},
		33: {"bar", "bar", "lemon"},
		34: {"grape", "bar", "lemon"},
		35: {"lemon", "bar", "lemon"},
		36: {"seven", "bar", "lemon"},
		37: {"bar", "grape", "lemon"},
		38: {"grape", "grape", "lemon"},
		39: {"lemon", "grape", "lemon"},
		40: {"seven", "grape", "lemon"},
		41: {"bar", "lemon", "lemon"},
		42: {"grape", "lemon", "lemon"},
		43: {"lemon", "lemon", "lemon"},
		44: {"seven", "lemon", "lemon"},
		45: {"bar", "seven", "lemon"},
		46: {"grape", "seven", "lemon"},
		47: {"lemon", "seven", "lemon"},
		48: {"seven", "seven", "lemon"},
		49: {"bar", "bar", "seven"},
		50: {"grape", "bar", "seven"},
		51: {"lemon", "bar", "seven"},
		52: {"seven", "bar", "seven"},
		53: {"bar", "grape", "seven"},
		54: {"grape", "grape", "seven"},
		55: {"lemon", "grape", "seven"},
		56: {"seven", "grape", "seven"},
		57: {"bar", "lemon", "seven"},
		58: {"grape", "lemon", "seven"},
		59: {"lemon", "lemon", "seven"},
		60: {"seven", "lemon", "seven"},
		61: {"bar", "seven", "seven"},
		62: {"grape", "seven", "seven"},
		63: {"lemon", "seven", "seven"},
		64: {"seven", "seven", "seven"},
	}
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), " ") // Split the line into fields.
		pulls, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, err
		}
		slotValue, ok := slotMachineValue[pulls]
		if !ok {
			return nil, fmt.Errorf("invalid pulls value: %d", pulls)
		}
		pullStats = append(pullStats, slotValue[0], slotValue[1], slotValue[2])
	}
	return pullStats, nil
}
func handleError(err error) {
	if err != nil {
		log.Println("Handled error!")
		log.Fatal(err)
	}
}
