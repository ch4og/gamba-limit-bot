package main

import (
	"bufio"
	"database/sql"
	"log"
	"os"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

const dbFilename = "data/gamba.db"

var db *sql.DB

func initDB() error {
	if err := os.MkdirAll("data", 0755); err != nil {
		return err
	}

	needsMigration := false
	if _, err := os.Stat(dbFilename); os.IsNotExist(err) {
		needsMigration = true
	}

	var err error
	db, err = sql.Open("sqlite", dbFilename)
	if err != nil {
		return err
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS gamblers (
			user_id     INTEGER PRIMARY KEY,
			gambles     INTEGER NOT NULL DEFAULT 0,
			gamble_time INTEGER NOT NULL DEFAULT 0,
			username    TEXT    NOT NULL DEFAULT '',
			wins        INTEGER NOT NULL DEFAULT 0,
			all_gambles INTEGER NOT NULL DEFAULT 0,
			notify_timer INTEGER NOT NULL DEFAULT 0,
			notified    INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS pull_stats (
			symbol TEXT PRIMARY KEY,
			count  INTEGER NOT NULL DEFAULT 0
		);
	`)
	if err != nil {
		return err
	}

	if needsMigration {
		migrateLegacyData()
	}

	return nil
}

func migrateLegacyData() {
	if _, err := os.Stat("gamba.txt"); err == nil {
		file, err := os.Open("gamba.txt")
		if err != nil {
			log.Printf("Failed to open gamba.txt for migration: %v", err)
		} else {
			count := 0
			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				fields := strings.Split(scanner.Text(), " ")
				if len(fields) < 8 {
					continue
				}
				userID, err := strconv.ParseInt(fields[0], 10, 64)
				if err != nil {
					continue
				}
				gambles, err := strconv.Atoi(fields[1])
				if err != nil {
					continue
				}
				gambleTime, err := strconv.ParseInt(fields[2], 10, 64)
				if err != nil {
					continue
				}
				wins, err := strconv.Atoi(fields[4])
				if err != nil {
					continue
				}
				allGambles, err := strconv.Atoi(fields[5])
				if err != nil {
					continue
				}
				notifyTimer, err := strconv.ParseBool(fields[6])
				if err != nil {
					continue
				}
				notified, err := strconv.ParseBool(fields[7])
				if err != nil {
					continue
				}
				db.Exec(
					`INSERT OR REPLACE INTO gamblers (user_id, gambles, gamble_time, username, wins, all_gambles, notify_timer, notified) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
					userID, gambles, gambleTime, fields[3], wins, allGambles, btoi(notifyTimer), btoi(notified),
				)
				count++
			}
			file.Close()
			log.Printf("Migrated %d gamblers from gamba.txt", count)
		}
	}

	if _, err := os.Stat("gamba_pulls.txt"); err == nil {
		file, err := os.Open("gamba_pulls.txt")
		if err != nil {
			log.Printf("Failed to open gamba_pulls.txt for migration: %v", err)
			return
		}
		counts := make(map[string]int64)
		slotMap := slotMachineValues()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			fields := strings.Split(scanner.Text(), " ")
			if len(fields) < 2 {
				continue
			}
			val, err := strconv.Atoi(fields[1])
			if err != nil {
				continue
			}
			symbols, ok := slotMap[val]
			if !ok {
				continue
			}
			for _, s := range symbols {
				counts[s]++
			}
		}
		file.Close()

		for symbol, count := range counts {
			db.Exec(
				`INSERT INTO pull_stats (symbol, count) VALUES (?, ?) ON CONFLICT(symbol) DO UPDATE SET count = count + ?`,
				symbol, count, count,
			)
		}
		log.Printf("Migrated pull stats from gamba_pulls.txt")
	}
}

func loadGamblerData() (map[int64]*Gambler, error) {
	rows, err := db.Query(`SELECT user_id, gambles, gamble_time, username, wins, all_gambles, notify_timer, notified FROM gamblers`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	gamblers := make(map[int64]*Gambler)
	for rows.Next() {
		var g Gambler
		var notifyTimer, notified int
		err := rows.Scan(&g.UserID, &g.Gambles, &g.GambleTime, &g.Username, &g.Wins, &g.AllGambles, &notifyTimer, &notified)
		if err != nil {
			return nil, err
		}
		g.NotifyTimer = notifyTimer != 0
		g.Notified = notified != 0
		gamblers[g.UserID] = &g
	}
	return gamblers, rows.Err()
}

func saveGamblerData(gamblers map[int64]*Gambler, gambaPull int, gambaPullUsername string) error {
	for _, g := range gamblers {
		_, err := db.Exec(
			`INSERT OR REPLACE INTO gamblers (user_id, gambles, gamble_time, username, wins, all_gambles, notify_timer, notified) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			g.UserID, g.Gambles, g.GambleTime, g.Username, g.Wins, g.AllGambles, btoi(g.NotifyTimer), btoi(g.Notified),
		)
		if err != nil {
			return err
		}
	}

	if gambaPull > 0 && gambaPullUsername != "" {
		symbols, ok := slotMachineValues()[gambaPull]
		if ok {
			for _, s := range symbols {
				_, err := db.Exec(
					`INSERT INTO pull_stats (symbol, count) VALUES (?, 1) ON CONFLICT(symbol) DO UPDATE SET count = count + 1`,
					s,
				)
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func loadPullStats() (map[string]int, error) {
	rows, err := db.Query(`SELECT symbol, count FROM pull_stats`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := make(map[string]int)
	for rows.Next() {
		var symbol string
		var count int
		err := rows.Scan(&symbol, &count)
		if err != nil {
			return nil, err
		}
		stats[symbol] = count
	}
	return stats, rows.Err()
}

func slotMachineValues() map[int][3]string {
	return map[int][3]string{
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
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
