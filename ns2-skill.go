package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/mux"
)

const nMatches int = 30
const nWeight int = 2
const scoreCutoff float64 = 100

var mysqlHostPort string = os.Getenv("MYSQL_HOST")
var mysqlUser string = os.Getenv("MYSQL_USER")
var mysqlPass string = os.Getenv("MYSQL_PASS")
var mysqlDb string = os.Getenv("MYSQL_DB")

var players map[int]*Player

type Round struct {
	roundId    int
	ns2id      int
	hiveSkill  int
	playerName string
	team       int
	win        int
}

type Player struct {
	ns2id             int
	name              string
	hiveskill         int
	nMarine           int
	nAlien            int
	weightMarine      float64
	weightAlien       float64
	repeatScoreMarine float64
	repeatScoreAlien  float64
	winrateMarine     float64
	winrateAlien      float64
	multiplierMarine  float64
	multiplierAlien   float64
	hiveskillMarine   int
	hiveskillAlien    int
}

type TeamComb struct {
	marines     []*Player
	aliens      []*Player
	diffMean    float64
	diffStd     float64
	score       float64
	repeatScore float64
}

type ShuffleResponse struct {
	Team1       []int             `json:"team1"`
	Team2       []int             `json:"team2"`
	Diagnostics map[string]string `json:"diagnostics"`
	Success     bool              `json:"success"`
	Msg         string            `json:"msg"`
}

type PlayerResponse struct {
	Ns2id       int `json:"ns2id"`
	MarineSkill int `json:"marine_skill"`
	AlienSkill  int `json:"alien_skill"`
}

type ShuffleRequest struct {
	Ns2ids     []int `json:"ns2ids"`
	Hiveskills []int `json:"hiveskills"`
}

func combs(n []int, emit func([]int, []int)) {
	sum := 0
	for _, c := range n {
		sum += c
	}
	var gen func([]int, []int, int)
	gen = func(n, res []int, pos int) {
		if pos == len(res) {
			x := make([][]int, len(n))
			for i, c := range res {
				x[c] = append(x[c], i)
			}

			emit(x[0], x[1])
			return
		}

		for i := range n {
			if n[i] == 0 {
				continue
			}
			n[i], res[pos] = n[i]-1, i
			gen(n, res, pos+1)
			n[i]++
		}
	}
	gen(n, make([]int, sum), 0)
}

func mean(vals ...int) float64 {
	var sum int
	for _, v := range vals {
		sum += v
	}
	avg := float64(sum / len(vals))
	return avg
}

func stdev(vals ...int) float64 {
	var n = float64(len(vals))
	var ss float64
	for _, v := range vals {
		ss += math.Pow(float64(v)-mean(vals...), 2)
	}
	return math.Pow(ss/n, 0.5)
}

func update() {
	players = make(map[int]*Player)
	db, err := sql.Open("mysql",
		fmt.Sprintf("%s:%s@tcp(%s)/%s", mysqlUser, mysqlPass, mysqlHostPort, mysqlDb))
	if err != nil {
		log.Fatal(err)
	}

	err = db.Ping()
	if err != nil {
		log.Fatal(err)
	}

	rows, err := db.Query("select prs.roundId, prs.steamId ns2id, ps.hiveSkill, prs.playerName, prs.lastTeam team, if(prs.lastTeam=winningTeam,1,0) win from PlayerRoundStats prs inner join RoundInfo ri on ri.roundId = prs.roundId inner join PlayerStats ps on ps.steamId = prs.steamId where ri.roundId > 1933 order by prs.roundId asc")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	playerAllRounds := make(map[int][]*Round)
	playerMarineRounds := make(map[int][]*Round)
	playerAlienRounds := make(map[int][]*Round)

	for rows.Next() {
		var round Round

		err := rows.Scan(&round.roundId, &round.ns2id, &round.hiveSkill, &round.playerName, &round.team, &round.win)
		if err != nil {
			log.Fatal(err.Error())
		} else {
			players[round.ns2id] = &Player{ns2id: round.ns2id, name: round.playerName, hiveskill: round.hiveSkill}
			playerAllRounds[round.ns2id] = append(playerAllRounds[round.ns2id], &round)
			switch round.team {
			case 1:
				playerMarineRounds[round.ns2id] = append(playerMarineRounds[round.ns2id], &round)
			case 2:
				playerAlienRounds[round.ns2id] = append(playerAlienRounds[round.ns2id], &round)
			}
		}
	}

	for team := 1; team <= 2; team++ {
		r := &playerMarineRounds

		if team == 2 {
			r = &playerAlienRounds
		}

		for ns2id, rounds := range *r {
			var wins int
			var winrate float64
			var n int = nMatches
			var nRounds int = len(rounds)
			var end int

			if nRounds >= n {
				end = nRounds - n
			} else {
				end = 0
				n = nRounds
			}
			for i := nRounds - 1; i >= end; i-- {
				wins += rounds[i].win
			}
			winrate = float64(wins) / float64(n)

			switch team {
			case 1:
				players[ns2id].nMarine = n
				players[ns2id].winrateMarine = winrate
			case 2:
				players[ns2id].nAlien = n
				players[ns2id].winrateAlien = winrate
			}

			roundsLen := len(playerAllRounds[ns2id])
			if roundsLen > 0 {
				var marineRounds, alienRounds int
				var lastTeam int = playerAllRounds[ns2id][roundsLen-1].team
				for i := roundsLen - 1; i >= 0; i-- {
					if playerAllRounds[ns2id][i].team == lastTeam {
						switch lastTeam {
						case 1:
							marineRounds += 1
						case 2:
							alienRounds += 1
						}
					} else {
						break
					}
				}
				players[ns2id].repeatScoreMarine = float64(marineRounds)
				players[ns2id].repeatScoreAlien = float64(alienRounds)
			} else {
				players[ns2id].repeatScoreMarine = 0
				players[ns2id].repeatScoreAlien = 0
			}
		}

		for _, p := range players {
			p.weightMarine = math.Pow(float64(p.nMarine)/float64(nMatches), 4)
			p.weightAlien = math.Pow(float64(p.nAlien)/float64(nMatches), 4)
			p.multiplierMarine = p.winrateMarine*2.0*p.weightMarine + (1.0 - p.weightMarine)
			p.multiplierAlien = p.winrateAlien*2.0*p.weightAlien + (1.0 - p.weightAlien)
			p.hiveskillMarine = int(float64(p.hiveskill) * p.multiplierMarine)
			p.hiveskillAlien = int(float64(p.hiveskill) * p.multiplierAlien)
		}
	}

	err = rows.Err()
	if err != nil {
		log.Fatal(err)
	}

	defer db.Close()
}

func shuffle(shuffle_ns2ids []int, shuffle_hiveskills []int) ShuffleResponse {
	var response ShuffleResponse

	if len(shuffle_ns2ids) <= 1 {
		response.Success = false
		response.Msg = "Can't shuffle with less than 2 players."
		return response
	}

	start := time.Now()
	update()
	for i, ns2id := range shuffle_ns2ids {
		if player, exists := players[ns2id]; !exists {
			players[ns2id] = &Player{ns2id: ns2id, hiveskill: shuffle_hiveskills[i], multiplierMarine: 1, multiplierAlien: 1, weightMarine: 0, weightAlien: 0}
		} else {
			player.hiveskill = shuffle_hiveskills[i]
			player.hiveskillMarine = int(float64(shuffle_hiveskills[i]) * player.multiplierMarine)
			player.hiveskillAlien = int(float64(shuffle_hiveskills[i]) * player.multiplierAlien)
		}
	}

	nTeamPlayers := len(shuffle_ns2ids) / 2

	var teamcombs []TeamComb

	combs([]int{nTeamPlayers, nTeamPlayers}, func(t1 []int, t2 []int) {
		var marines []*Player
		var aliens []*Player
		var meanMarines, meanAliens, stdMarines, stdAliens float64
		var diffMean, diffStd, score float64
		var marineRepeatScore, alienRepeatScore, repeatScore float64
		var hiveskillsMarine, hiveskillsAlien []int

		for _, i := range t1 {
			player := players[shuffle_ns2ids[i]]
			player.hiveskillMarine = int(float64(player.hiveskill) * player.multiplierMarine)
			marines = append(marines, player)
			var hs int = player.hiveskillMarine
			hiveskillsMarine = append(hiveskillsMarine, hs)
			marineRepeatScore += player.repeatScoreMarine
		}
		for _, i := range t2 {
			player := players[shuffle_ns2ids[i]]
			player.hiveskillAlien = int(float64(player.hiveskill) * player.multiplierAlien)
			aliens = append(aliens, player)
			var hs int = player.hiveskillAlien
			hiveskillsAlien = append(hiveskillsAlien, hs)
			alienRepeatScore += player.repeatScoreAlien
		}

		meanMarines = mean(hiveskillsMarine...)
		meanAliens = mean(hiveskillsAlien...)
		stdMarines = stdev(hiveskillsMarine...)
		stdAliens = stdev(hiveskillsAlien...)
		diffMean = math.Abs(meanMarines - meanAliens)
		diffStd = math.Abs(stdMarines - stdAliens)
		score = math.Sqrt(math.Pow(diffMean, 2) + math.Pow(diffStd, 2))
		repeatScore = marineRepeatScore + alienRepeatScore

		var tc TeamComb = TeamComb{marines: marines, aliens: aliens, diffMean: diffMean, diffStd: diffStd, score: score, repeatScore: repeatScore}
		teamcombs = append(teamcombs, tc)
	})

	var cutoffCombs []TeamComb

	for _, comb := range teamcombs {
		if comb.score < scoreCutoff {
			cutoffCombs = append(cutoffCombs, comb)
		}
	}

	var bestComb TeamComb = TeamComb{score: 9999, repeatScore: 9999}
	if len(cutoffCombs) > 1 {
		for _, comb := range cutoffCombs {
			if comb.repeatScore < bestComb.repeatScore {
				bestComb = comb
			}
		}
	} else {
		for _, comb := range teamcombs {
			if comb.score < bestComb.score {
				bestComb = comb
			}
		}
	}

	var t1, t2 []int

	for _, p := range bestComb.marines {
		t1 = append(t1, p.ns2id)
	}
	for _, p := range bestComb.aliens {
		t2 = append(t2, p.ns2id)
	}

	elapsed := time.Since(start)
	diagnostics := make(map[string]string)
	diagnostics["Version"] = "1.1"
	diagnostics["Time elapsed"] = fmt.Sprintf("%s", elapsed)
	diagnostics["Score"] = fmt.Sprintf("%.2f", bestComb.score)
	diagnostics["RScore"] = fmt.Sprintf("%.2f", bestComb.repeatScore)

	response.Team1 = t1
	response.Team2 = t2
	response.Diagnostics = diagnostics

	response.Success = true
	response.Msg = "Shuffled successfully."

	return response

}

func ShuffleEndpoint(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	ns2idsString := r.Form["ns2ids"][0]
	hsString := r.Form["hiveskills"][0]

	var ns2ids, hiveskills []int
	json.Unmarshal([]byte(ns2idsString), &ns2ids)
	json.Unmarshal([]byte(hsString), &hiveskills)

	log.Println(fmt.Sprintf("Requested shuffle with %d players", len(ns2ids)))

	response := shuffle(ns2ids, hiveskills)
	jsonResponse, _ := json.Marshal(response)
	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonResponse)
}

func PlayerEndpoint(w http.ResponseWriter, r *http.Request) {
	var playerName string
	r.ParseForm()
	ns2id, _ := strconv.Atoi(r.Form["ns2id"][0])
	hs, _ := strconv.Atoi(r.Form["hiveskill"][0])

	player, playerExists := players[ns2id]
	if playerExists {
		player.hiveskill = hs
		player.hiveskillMarine = int(float64(hs) * player.multiplierMarine)
		player.hiveskillAlien = int(float64(hs) * player.multiplierAlien)
		playerName = player.name
	} else {
		player = &Player{ns2id: ns2id, hiveskill: hs, hiveskillMarine: hs, hiveskillAlien: hs}
		playerName = "<New player>"
	}
	response := &PlayerResponse{MarineSkill: player.hiveskillMarine, AlienSkill: player.hiveskillAlien, Ns2id: ns2id}
	jsonResponse, _ := json.Marshal(response)
	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonResponse)

	log.Println(fmt.Sprintf("Requested player data for %s (%d): Marine: %d - Alien: %d", playerName, ns2id, player.hiveskillMarine, player.hiveskillAlien))
}

func main() {
	update()
	r := mux.NewRouter()
	r.HandleFunc("/shuffle", ShuffleEndpoint).Methods("POST")
	r.HandleFunc("/player/scoreboard_data", PlayerEndpoint).Methods("POST")
	if err := http.ListenAndServe(":3000", r); err != nil {
		log.Fatal(err)
	}
}
