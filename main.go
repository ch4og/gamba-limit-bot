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
}
const limit_delay = 2.5
func main() {
	// TODO: command to enable pm notifications about timer reset (maybe)
	
	// Load .env file
	err := godotenv.Load()
	handleError(err)

	// Create bot
	telegram_token := os.Getenv("TELEGRAM_API_TOKEN")
	bot, err := tgbotapi.NewBotAPI(telegram_token)
	handleError(err)

	log.Printf("Authorized on account %s", bot.Self.UserName)

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
	err := sendMessageAndDeleteAfterDelay(bot, chatID, msgID, topText, 60)
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
		}
		gamblers[update.Message.From.ID] = gambler
	}

	// Check if the gambling limit has been reached
	timeSince := time.Since(time.Unix(gambler.GambleTime, 0))
	if timeSince.Minutes() < 60 {
		gambler.Gambles++ // Increment the number of gambles
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

		err := sendMessageAndDeleteAfterDelay(bot, update.Message.Chat.ID, update.Message.MessageID, msg_text, limit_delay)
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
		err := saveGamblerData(gamblers)
		return err
	}
}
func saveGamblerData(gamblers map[int64]*Gambler) error {
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
        line := fmt.Sprintf("%d %d %d %s %d %d\n",
            UserID,
            gambler.Gambles,
            gambler.GambleTime,
            gambler.Username,
            gambler.Wins,
            gambler.AllGambles,
        )

		// Write the line to the file.
        _, err = file.WriteString(line)
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

		// Create a new gambler and add it to the map.
		gambler := &Gambler{
			UserID:      UserID,
			Gambles:     Gambles,
			GambleTime:  GambleTime,
			Username:    fields[3],
			Wins:        Wins,
			AllGambles:  AllGambles,
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
func sendMessageAndDeleteAfterDelay(bot *tgbotapi.BotAPI, chatID int64, messageID int, text string, delay_time float64) error {
	// Create the message to send
	var deleteSticker tgbotapi.DeleteMessageConfig
	doStickerExist := false
	message := tgbotapi.NewMessage(chatID, text)
	message.DisableNotification = true 

	// Delete the original message
	bot.Send(tgbotapi.NewDeleteMessage(chatID, messageID))

	if rand.IntN(10) == 4 && delay_time == limit_delay {
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
func handleError(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
