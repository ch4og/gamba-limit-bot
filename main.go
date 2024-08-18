package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
)

	type Gambler struct {
		userid int64
		gambles int
		gamble_hour int64
		username string
		wins int
		all_gambles int
	}
func main() {
	// TODO: sort by luck && command to enable pm notifications about timer reset
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	telegram_token := os.Getenv("TELEGRAM_API_TOKEN")
	bot, err := tgbotapi.NewBotAPI(telegram_token)
	if err != nil {
		log.Panic(err)
	}

	log.Printf("Authorized on account %s", bot.Self.UserName)

	gamblers, err := load_gamba()
	if err != nil {
		log.Fatal(err)
	}
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)
	for update := range updates {
		if update.Message.Text == "/top" {
			top_text := new_top(gamblers)
			if err != nil {
				log.Fatal(err)
			}
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, top_text)
			bot.Send(msg)
		} else if update.Message.Dice != nil { 
			if update.Message.ForwardFrom == nil {
				if update.Message.Dice.Emoji == "🎰" {
					userid := update.Message.From.ID
					username := update.Message.From.UserName
					gambler, ok := gamblers[userid]
					if !ok {
						gambler = &Gambler{userid: userid, gambles: 0, gamble_hour: time.Now().Unix(), username: username, wins: 0}
						gamblers[userid] = gambler
					}
					if time.Since(time.Unix(gambler.gamble_hour, 0)).Minutes() < 60 {
						gambler.gambles++
					} else {
						gambler.gambles = 1
						gambler.gamble_hour = time.Now().Unix()
					}
					if gambler.gambles > 3 {
						username := update.Message.From.UserName
						minutes := int(60 - time.Since(time.Unix(gambler.gamble_hour, 0)).Minutes())
						seconds := int(time.Since(time.Unix(gambler.gamble_hour, 0)).Seconds()) % 60
						msg_text := fmt.Sprintf("%s ЛИМИТ ГАМБЫ ПРЕВЫШЕН!\nПравила буна: 3 крутки в час\n\nПопробуйте снова через %d мин %d сек!\n", username, minutes, seconds)
						msg := tgbotapi.NewMessage(update.Message.Chat.ID, msg_text)
						msg.DisableNotification = true
						del_gamba := tgbotapi.NewDeleteMessage(update.Message.Chat.ID, update.Message.MessageID)
						bot.Send(del_gamba)
						msg_sent, err := bot.Send(msg)
						if err != nil {
							log.Printf("Error sending message: %v", err)
						}
						del_msg := tgbotapi.NewDeleteMessage(update.Message.Chat.ID, msg_sent.MessageID)
						go func() {
							time.Sleep(time.Duration(25 / 10  * time.Second))
							bot.Send(del_msg)
						}()
					} else {
						switch update.Message.Dice.Value {
						case 1, 22, 43, 64:
							gambler.wins++
						}
						gambler.all_gambles++
						err = save_gamba(gamblers)
						if err != nil {
							log.Fatal(err)
						}
					}
				}
			}
		}
	}

}
func save_gamba(gamblers map[int64]*Gambler) error {
    filename := "gamba.txt"
    file, err := os.Create(filename)
    if err != nil {
        return err
    }
    defer file.Close()

    for k, v := range gamblers {
        _, err = file.WriteString(fmt.Sprintf("%d %d %d %s %d %d\n", k, v.gambles, v.gamble_hour, v.username, v.wins, v.all_gambles))
        if err != nil {
            return err
        }
    }

    return nil
}
func load_gamba() (map[int64]*Gambler, error) {
    filename := "gamba.txt"
    file, err := os.Open(filename)
    if err != nil {
        return nil, err
    }
    defer file.Close()

    gamblers := make(map[int64]*Gambler)
    scanner := bufio.NewScanner(file)
    for scanner.Scan() {
        fields := strings.Split(scanner.Text(), " ")
        k, _ := strconv.ParseInt(fields[0], 10, 64)
        gambles, _ := strconv.Atoi(fields[1])
        gamble_hour, _ := strconv.ParseInt(fields[2], 10, 64)
        wins, _ := strconv.Atoi(fields[4])
        all_gambles, _ := strconv.Atoi(fields[5])
        v := &Gambler{
            userid:    k,
            gambles:   gambles,
            gamble_hour: gamble_hour,
            username:  fields[3],
            wins:      wins,
			all_gambles: all_gambles,
        }
        gamblers[k] = v
    }

    return gamblers, nil
}

func new_top(gamblers map[int64]*Gambler) (string) {
	var newtop = ""
	var gamblerSlice []*Gambler
	for _, gambler := range gamblers {
		gamblerSlice = append(gamblerSlice, gambler)
	}

	sort.Slice(gamblerSlice, func(i, j int) bool {
		return gamblerSlice[i].wins > gamblerSlice[j].wins
	})

	newtop = "Правила буна: 3 крутки в час\n\n🎰 ТОП ГАМБЫ\n\n"
	for _, gambler := range gamblerSlice {
		newtop += fmt.Sprintf("%s - %d побед - %d круток\n", gambler.username, gambler.wins, gambler.all_gambles)
	}
	return newtop
}