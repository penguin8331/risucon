package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo-contrib/session"
	"github.com/labstack/echo/v4"
)

var (
	// Subtask は update がないのでキャッシュしておく
	// メモ: initializeHandler でキャッシュを消すのを忘れずに
	subtaskcache = sync.Map{}

	standingssubcache       = sync.Map{}
	standingssubexistscache = sync.Map{}
	usercache               = sync.Map{}
	subtaskmaxscorecache    = sync.Map{}
)

type Task struct {
	ID              int    `db:"id"`
	Name            string `db:"name"`
	DisplayName     string `db:"display_name"`
	Statement       string `db:"statement"`
	SubmissionLimit int    `db:"submission_limit"`
}
type Subtask struct {
	ID          int    `db:"id"`
	Name        string `db:"name"`
	DisplayName string `db:"display_name"`
	TaskID      int    `db:"task_id"`
	Statement   string `db:"statement"`
}
type Answer struct {
	ID        int    `db:"id"`
	TaskID    int    `db:"task_id"`
	SubtaskID int    `db:"subtask_id"`
	Answer    string `db:"answer"`
	Score     int    `db:"score"`
}
type Submission struct {
	ID          int       `db:"id"`
	TaskID      int       `db:"task_id"`
	UserID      int       `db:"user_id"`
	SubmittedAt time.Time `db:"submitted_at"`
	Answer      string    `db:"answer"`
	SubTaskID   int       `db:"subtask_id"`
	Score       int       `db:"score"`
}

type TaskAbstract struct {
	Name            string `json:"name"`
	DisplayName     string `json:"display_name"`
	MaxScore        int    `json:"max_score"`
	Score           int    `json:"score,omitempty"`
	SubmissionLimit int    `json:"submission_limit,omitempty"`
	SubmissionCount int    `json:"submission_count,omitempty"`
}

func gettaskabstarcts(ctx context.Context, c echo.Context) ([]TaskAbstract, error) {
	tasks := []Task{}
	if err := dbConn.SelectContext(ctx, &tasks, "SELECT * FROM tasks ORDER BY name"); err != nil {
		return []TaskAbstract{}, err
	}
	res := []TaskAbstract{}
	for _, task := range tasks {
		maxscore := 0
		subtasks := []Subtask{}
		if s, ok := subtaskcache.Load(task.ID); ok {
			subtasks = s.([]Subtask)
		} else {
			if err := dbConn.SelectContext(ctx, &subtasks, "SELECT * FROM subtasks WHERE task_id = ?", task.ID); err != nil {
				return []TaskAbstract{}, err
			}
			subtaskcache.Store(task.ID, subtasks)
		}
		for _, subtask := range subtasks {
			maxscore_for_subtask := 0
			if msc, ok := subtaskmaxscorecache.Load(subtask.ID); ok {
				maxscore_for_subtask = msc.(int)
			} else {
				if err := dbConn.GetContext(ctx, &maxscore_for_subtask, "SELECT MAX(score) FROM answers WHERE subtask_id = ?", subtask.ID); err != nil {
					return []TaskAbstract{}, err
				}
				subtaskmaxscorecache.Store(subtask.ID, maxscore_for_subtask)
			}
			maxscore += maxscore_for_subtask
		}
		submissioncount := 0
		score := 0
		if err := verifyUserSession(c); err == nil {
			sess, _ := session.Get(defaultSessionIDKey, c)
			username, _ := sess.Values[defaultSessionUserNameKey].(string)
			user := User{}
			if err := dbConn.GetContext(c.Request().Context(), &user, "SELECT * FROM users WHERE name = ?", username); err != nil {
				return []TaskAbstract{}, err
			}
			team := Team{}
			err := dbConn.GetContext(c.Request().Context(), &team, "SELECT * FROM teams WHERE leader_id = ? OR member1_id = ? OR member2_id = ?", user.ID, user.ID, user.ID)
			if err == nil {
				err := dbConn.GetContext(c.Request().Context(), &submissioncount, "SELECT COUNT(*) FROM submissions WHERE task_id = ? AND user_id IN (?,?,?)", task.ID, team.LeaderID, team.Member1ID, team.Member2ID)
				if err != nil {
					return []TaskAbstract{}, err
				}

				if sc, ok := standingssubcache.Load(team.ID*10000 + task.ID); ok {
					score = sc.(int)
				} else {
					sc := []int{}
					if err := dbConn.SelectContext(ctx, &sc, "SELECT MAX(score) FROM submissions WHERE task_id = ? AND user_id IN (?,?,?) GROUP BY subtask_id;", task.ID, team.LeaderID, team.Member1ID, team.Member2ID); err != nil {
						return []TaskAbstract{}, err
					}
					for _, s := range sc {
						score += s
					}
					standingssubcache.Store(team.ID*10000+task.ID, score)
				}
			} else if err != sql.ErrNoRows {
				return []TaskAbstract{}, err
			}
		}
		res = append(res, TaskAbstract{
			Name:            task.Name,
			DisplayName:     task.DisplayName,
			MaxScore:        maxscore,
			Score:           score,
			SubmissionLimit: task.SubmissionLimit,
			SubmissionCount: submissioncount,
		})
	}

	return res, nil
}

// GET /api/tasks
func getTasksHandler(c echo.Context) error {
	ctx := c.Request().Context()

	taskabstarcts, err := gettaskabstarcts(ctx, c)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get taskabstarcts: "+err.Error())
	}

	return c.JSON(http.StatusOK, taskabstarcts)
}

type TeamsStandingsSub struct {
	TaskName     string `json:"task_name"`
	HasSubmitted bool   `json:"has_submitted"`
	Score        int    `json:"score"`
}
type TeamsStandings struct {
	Rank               int                 `json:"rank"`
	TeamName           string              `json:"team_name"`
	TeamDisplayName    string              `json:"team_display_name"`
	LeaderName         string              `json:"leader_name"`
	LeaderDisplayName  string              `json:"leader_display_name"`
	Member1Name        string              `json:"member1_name,omitempty"`
	Member1DisplayName string              `json:"member1_display_name,omitempty"`
	Member2Name        string              `json:"member2_name,omitempty"`
	Member2DisplayName string              `json:"member2_display_name,omitempty"`
	ScoringData        []TeamsStandingsSub `json:"scoring_data"`
	TotalScore         int                 `json:"total_score"`
}
type Standings struct {
	TasksData     []TaskAbstract   `json:"tasks_data"`
	StandingsData []TeamsStandings `json:"standings_data"`
}

func getstandings(ctx context.Context) (Standings, error) {
	standings := Standings{}

	tasks := []Task{}
	if err := dbConn.SelectContext(ctx, &tasks, "SELECT * FROM tasks ORDER BY name"); err != nil {
		return Standings{}, err
	}
	for _, task := range tasks {
		/*subtasks := []Subtask{}
		if err := dbConn.SelectContext(ctx, &subtasks, "SELECT * FROM subtasks WHERE task_id = ?", task.ID); err != nil {
			return Standings{}, err
		}
		maxscore := 0
		for _, subtask := range subtasks {
			subtaskmaxscore := 0
			if err := dbConn.GetContext(ctx, &subtaskmaxscore, "SELECT MAX(score) FROM answers WHERE subtask_id = ?", subtask.ID); err != nil {
				return Standings{}, err
			}
			maxscore += subtaskmaxscore
		}*/
		maxscore := 0
		sc := []int{}
		if err := dbConn.SelectContext(ctx, &sc, "SELECT MAX(score) FROM answers WHERE task_id = ? GROUP BY subtask_id", task.ID); err != nil {
			return Standings{}, err
		}
		for _, s := range sc {
			maxscore += s
		}

		standings.TasksData = append(standings.TasksData, TaskAbstract{
			Name:        task.Name,
			DisplayName: task.DisplayName,
			MaxScore:    maxscore,
		})
	}

	teams := []Team{}
	if err := dbConn.SelectContext(ctx, &teams, "SELECT * FROM teams ORDER BY name"); err != nil {
		return Standings{}, err
	}
	for _, team := range teams {
		teamstandings := TeamsStandings{}
		teamstandings.TeamName = team.Name
		teamstandings.TeamDisplayName = team.DisplayName
		teamstandings.TotalScore = 0

		leader := User{}
		if u, ok := usercache.Load(team.LeaderID); ok {
			leader = u.(User)
		} else {
			if err := dbConn.GetContext(ctx, &leader, "SELECT * FROM users WHERE id = ?", team.LeaderID); err != nil {
				return Standings{}, err
			}
			usercache.Store(team.LeaderID, leader)
		}

		teamstandings.LeaderName = leader.Name
		teamstandings.LeaderDisplayName = leader.DisplayName
		if team.Member1ID != nulluserid {
			member1 := User{}
			if u, ok := usercache.Load(team.Member1ID); ok {
				member1 = u.(User)
			} else {
				if err := dbConn.GetContext(ctx, &member1, "SELECT * FROM users WHERE id = ?", team.Member1ID); err != nil {
					return Standings{}, err
				}
				usercache.Store(team.Member1ID, member1)
			}
			teamstandings.Member1Name = member1.Name
			teamstandings.Member1DisplayName = member1.DisplayName
		}
		if team.Member2ID != nulluserid {
			member2 := User{}
			if u, ok := usercache.Load(team.Member2ID); ok {
				member2 = u.(User)
			} else {
				if err := dbConn.GetContext(ctx, &member2, "SELECT * FROM users WHERE id = ?", team.Member2ID); err != nil {
					return Standings{}, err
				}
				usercache.Store(team.Member2ID, member2)
			}
			teamstandings.Member2Name = member2.Name
			teamstandings.Member2DisplayName = member2.DisplayName
		}

		scoringdata := []TeamsStandingsSub{}
		for _, task := range tasks {
			taskscoringdata := TeamsStandingsSub{}
			taskscoringdata.TaskName = task.Name
			taskscoringdata.HasSubmitted = false
			taskscoringdata.Score = 0

			subtasks := []Subtask{}
			if s, ok := subtaskcache.Load(task.ID); ok {
				subtasks = s.([]Subtask)
			} else {
				if err := dbConn.SelectContext(ctx, &subtasks, "SELECT * FROM subtasks WHERE task_id = ?", task.ID); err != nil {
					return Standings{}, err
				}
				subtaskcache.Store(task.ID, subtasks)
			}
			if b, ok := standingssubexistscache.Load(team.ID*10000 + task.ID); ok {
				taskscoringdata.HasSubmitted = b.(bool)
			} else {
				submissioncount := 0
				if err := dbConn.GetContext(ctx, &submissioncount, "SELECT COUNT(*) FROM submissions WHERE task_id = ? AND user_id IN (?,?,?) LIMIT 1", task.ID, team.LeaderID, team.Member1ID, team.Member2ID); err != nil {
					return Standings{}, err
				}
				if submissioncount > 0 {
					taskscoringdata.HasSubmitted = true
				}
				standingssubexistscache.Store(team.ID*10000+task.ID, taskscoringdata.HasSubmitted)
			}

			if sc, ok := standingssubcache.Load(team.ID*10000 + task.ID); ok {
				taskscoringdata.Score = sc.(int)
			} else {
				sc := []int{}
				if err := dbConn.SelectContext(ctx, &sc, "SELECT MAX(score) FROM submissions WHERE task_id = ? AND user_id IN (?,?,?) GROUP BY subtask_id;", task.ID, team.LeaderID, team.Member1ID, team.Member2ID); err != nil {
					return Standings{}, err
				}
				for _, s := range sc {
					taskscoringdata.Score += s
				}
				standingssubcache.Store(team.ID*10000+task.ID, taskscoringdata.Score)
			}

			scoringdata = append(scoringdata, taskscoringdata)
			teamstandings.TotalScore += taskscoringdata.Score
		}
		teamstandings.ScoringData = scoringdata
		standings.StandingsData = append(standings.StandingsData, teamstandings)
	}

	// sort
	for i := 0; i < len(standings.StandingsData); i++ {
		for j := i + 1; j < len(standings.StandingsData); j++ {
			if (standings.StandingsData[i].TotalScore < standings.StandingsData[j].TotalScore) || (standings.StandingsData[i].TotalScore == standings.StandingsData[j].TotalScore && standings.StandingsData[i].TeamName > standings.StandingsData[j].TeamName) {
				tmp := standings.StandingsData[i]
				standings.StandingsData[i] = standings.StandingsData[j]
				standings.StandingsData[j] = tmp
			}
		}
	}
	for i := 0; i < len(standings.StandingsData); i++ {
		standings.StandingsData[i].Rank = 1
		for j := 0; j < len(standings.StandingsData); j++ {
			if standings.StandingsData[i].TotalScore < standings.StandingsData[j].TotalScore {
				standings.StandingsData[i].Rank++
			}
		}
	}

	return standings, nil
}

// GET /api/stanings
func getStandingsHandler(c echo.Context) error {
	ctx := c.Request().Context()

	standings, err := getstandings(ctx)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get standings: "+err.Error())
	}

	return c.JSON(http.StatusOK, standings)
}

type SubtaskDetail struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Statement   string `json:"statement"`
	MaxScore    int    `json:"max_score"`
	Score       int    `json:"score"`
}
type TaskDetail struct {
	Name            string          `json:"name"`
	DisplayName     string          `json:"display_name"`
	Statement       string          `json:"statement"`
	MaxScore        int             `json:"max_score"`
	Score           int             `json:"score"`
	SubmissionLimit int             `json:"submission_limit"`
	SubmissionCount int             `json:"submission_count"`
	Subtasks        []SubtaskDetail `json:"subtasks"`
}

// GET /api/tasks/:taskname
func getTaskHandler(c echo.Context) error {
	taskname := c.Param("taskname")

	task := Task{}

	err := dbConn.GetContext(c.Request().Context(), &task, "SELECT * FROM tasks WHERE name = ?", taskname)

	if err == sql.ErrNoRows {
		return echo.NewHTTPError(http.StatusNotFound, "task not found")
	} else if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get task: "+err.Error())
	}

	subtasks := []Subtask{}

	if cache_data, ok := subtaskcache.Load(task.ID); ok {
		// データがキャッシュされているので、それを読み込む
		subtasks = cache_data.([]Subtask)
	} else {
		if err := dbConn.SelectContext(c.Request().Context(), &subtasks, "SELECT * FROM subtasks WHERE task_id = ?", task.ID); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to get subtasks: "+err.Error())
		}

		// キャッシュにデータを保存
		subtaskcache.Store(task.ID, subtasks)
	}

	res := TaskDetail{
		Name:            task.Name,
		DisplayName:     task.DisplayName,
		Statement:       task.Statement,
		SubmissionLimit: task.SubmissionLimit,
		MaxScore:        0,
		Score:           0,
		Subtasks:        []SubtaskDetail{},
		SubmissionCount: 0,
	}

	for _, subtask := range subtasks {
		subtaskdetail := SubtaskDetail{
			Name:        subtask.Name,
			DisplayName: subtask.DisplayName,
			Statement:   subtask.Statement,
		}
		if msc, ok := subtaskmaxscorecache.Load(subtask.ID); ok {
			subtaskdetail.MaxScore = msc.(int)
		} else {
			if err := dbConn.GetContext(c.Request().Context(), &subtaskdetail.MaxScore, "SELECT MAX(score) FROM answers WHERE subtask_id = ?", subtask.ID); err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to get subtask score: "+err.Error())
			}
			subtaskmaxscorecache.Store(subtask.ID, subtaskdetail.MaxScore)
		}
		res.Subtasks = append(res.Subtasks, subtaskdetail)
		res.MaxScore += subtaskdetail.MaxScore
	}

	if err := verifyUserSession(c); err == nil {
		sess, _ := session.Get(defaultSessionIDKey, c)
		username, _ := sess.Values[defaultSessionUserNameKey].(string)
		user := User{}
		if err := dbConn.GetContext(c.Request().Context(), &user, "SELECT * FROM users WHERE name = ?", username); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to get user: "+err.Error())
		}
		team := Team{}
		err := dbConn.GetContext(c.Request().Context(), &team, "SELECT * FROM teams WHERE leader_id = ? OR member1_id = ? OR member2_id = ?", user.ID, user.ID, user.ID)
		if err == nil {
			err := dbConn.GetContext(c.Request().Context(), &res.SubmissionCount, "SELECT COUNT(*) FROM submissions WHERE task_id = ? AND user_id IN (?,?,?)", task.ID, team.LeaderID, team.Member1ID, team.Member2ID)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to get submission count: "+err.Error())
			}

			for i, subtask := range subtasks {
				subtaskscore := 0
				if err := dbConn.GetContext(c.Request().Context(), &subtaskscore, "SELECT COALESCE(MAX(score),0) FROM submissions WHERE subtask_id = ? AND user_id IN (?,?,?)", subtask.ID, team.LeaderID, team.Member1ID, team.Member2ID); err != nil {
					return echo.NewHTTPError(http.StatusInternalServerError, "failed to get subtask score: "+err.Error())
				}
				res.Subtasks[i].Score = subtaskscore
				res.Score += subtaskscore
			}
		} else if err != sql.ErrNoRows {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to get team: "+err.Error())
		}
	}

	return c.JSON(http.StatusOK, res)
}

type SubmitRequest struct {
	TaskName  string `json:"task_name"`
	Answer    string `json:"answer"`
	Timestamp int64  `json:"timestamp"`
}

type SubmitResponse struct {
	IsScored             bool   `json:"is_scored"`
	Score                int    `json:"score"`
	SubtaskName          string `json:"subtask_name,omitempty"`
	SubTaskDisplayName   string `json:"subtask_display_name,omitempty"`
	SubTaskMaxScore      int    `json:"subtask_max_score,omitempty"`
	RemainingSubmissions int    `json:"remaining_submissions"`
}

// POST /api/submit
func submitHandler(c echo.Context) error {
	ctx := c.Request().Context()
	defer c.Request().Body.Close()

	if err := verifyUserSession(c); err != nil {
		return err
	}

	sess, _ := session.Get(defaultSessionIDKey, c)
	username, _ := sess.Values[defaultSessionUserNameKey].(string)

	tx, err := dbConn.BeginTxx(c.Request().Context(), nil)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to begin transaction: "+err.Error())
	}
	defer tx.Rollback()

	user := User{}
	if err := tx.GetContext(c.Request().Context(), &user, "SELECT * FROM users WHERE name = ?", username); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get user: "+err.Error())
	}

	team := Team{}
	err = tx.GetContext(c.Request().Context(), &team, "SELECT * FROM teams WHERE leader_id = ? OR member1_id = ? OR member2_id = ?", user.ID, user.ID, user.ID)
	if err == sql.ErrNoRows {
		return echo.NewHTTPError(http.StatusBadRequest, "you have not joined team")
	} else if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get team: "+err.Error())
	}

	req := SubmitRequest{}
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to decode the request body as json")
	}

	task := Task{}
	err = tx.GetContext(c.Request().Context(), &task, "SELECT * FROM tasks WHERE name = ?", req.TaskName)
	if err == sql.ErrNoRows {
		return echo.NewHTTPError(http.StatusBadRequest, "task not found")
	} else if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get task: "+err.Error())
	}

	submissionscount := 0
	if err := tx.GetContext(c.Request().Context(), &submissionscount, "SELECT COUNT(*) FROM submissions WHERE task_id = ? AND user_id IN (?,?,?)", task.ID, team.LeaderID, team.Member1ID, team.Member2ID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get submissions count: "+err.Error())
	}

	if submissionscount >= task.SubmissionLimit {
		return echo.NewHTTPError(http.StatusBadRequest, "submission limit exceeded")
	}

	res := SubmitResponse{}

	// デフォルトではこれを返す。答えが有効な場合は更新される。
	res.IsScored = false
	res.Score = 0
	res.RemainingSubmissions = task.SubmissionLimit - submissionscount - 1

	subtasks := []Subtask{}
	if s, ok := subtaskcache.Load(task.ID); ok {
		subtasks = s.([]Subtask)
	} else {
		if err := tx.SelectContext(c.Request().Context(), &subtasks, "SELECT * FROM subtasks WHERE task_id = ?", task.ID); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to get subtasks: "+err.Error())
		}
		subtaskcache.Store(task.ID, subtasks)
	}
	subtaskid := -1
	for _, subtask := range subtasks {
		answers := []Answer{}
		if err := tx.SelectContext(c.Request().Context(), &answers, "SELECT * FROM answers WHERE subtask_id = ?", subtask.ID); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to get answers: "+err.Error())
		}
		// SubTaskMaxScore は事前に計算しておく
		subtaskmaxscore := 0
		for _, answer := range answers {
			if subtaskmaxscore < answer.Score {
				subtaskmaxscore = answer.Score
			}
		}
		// 答えが有効な場合、スコアを更新する
		for _, answer := range answers {
			if answer.Answer == req.Answer {
				res.IsScored = true
				res.Score = answer.Score
				res.SubtaskName = subtask.Name
				res.SubTaskDisplayName = subtask.DisplayName
				res.SubTaskMaxScore = subtaskmaxscore
				subtaskid = subtask.ID
			}
		}
	}

	timestamp := time.Unix(req.Timestamp, 0)

	if _, err = tx.ExecContext(ctx, "INSERT INTO submissions (task_id, user_id, submitted_at, answer, subtask_id, score) VALUES (?, ?, ?, ?, ?, ?)", task.ID, user.ID, timestamp, req.Answer, subtaskid, res.Score); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to insert submission: "+err.Error())
	}

	standingssubexistscache.Store(team.ID*10000+task.ID, true)
	if res.IsScored {
		standingssubcache.Delete(team.ID*10000 + task.ID)
	}

	if err := tx.Commit(); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to commit transaction: "+err.Error())
	}

	return c.JSON(http.StatusCreated, res)
}

type SubmissionDetail struct {
	TaskName           string `json:"task_name"`
	TaskDisplayName    string `json:"task_display_name"`
	SubTaskName        string `json:"subtask_name"`
	SubTaskDisplayName string `json:"subtask_display_name"`
	SubTaskMaxScore    int    `json:"subtask_max_score"`
	UserName           string `json:"user_name"`
	UserDisplayName    string `json:"user_display_name"`
	SubmittedAt        int64  `json:"submitted_at"`
	Answer             string `json:"answer"`
	Score              int    `json:"score"`
}

type submissionresponse struct {
	Submissions     []SubmissionDetail `json:"submissions"`
	SubmissionCount int                `json:"submission_count"`
}

// GET /api/submissions
func getSubmissionsHandler(c echo.Context) error {
	submissionsperpage := 20
	if err := verifyUserSession(c); err != nil {
		return err
	}

	sess, _ := session.Get(defaultSessionIDKey, c)
	username, _ := sess.Values[defaultSessionUserNameKey].(string)

	user := User{}
	if err := dbConn.GetContext(c.Request().Context(), &user, "SELECT * FROM users WHERE name = ?", username); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get user: "+err.Error())
	}

	team := Team{}
	if username != "admin" {
		err := dbConn.GetContext(c.Request().Context(), &team, "SELECT * FROM teams WHERE leader_id = ? OR member1_id = ? OR member2_id = ?", user.ID, user.ID, user.ID)
		if err == sql.ErrNoRows {
			return echo.NewHTTPError(http.StatusBadRequest, "you have not joined team")
		} else if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to get team: "+err.Error())
		}
	} else if c.QueryParam("team_name") != "" {
		err := dbConn.GetContext(c.Request().Context(), &team, "SELECT * FROM teams WHERE name = ?", c.QueryParam("team_name"))
		if err == sql.ErrNoRows {
			return echo.NewHTTPError(http.StatusBadRequest, "team not found")
		} else if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to get team: "+err.Error())
		}
	}

	conditions := make([]string, 0)
	params := make([]interface{}, 0)

	if c.QueryParam("task_name") != "" {
		task := Task{}
		err := dbConn.GetContext(c.Request().Context(), &task, "SELECT * FROM tasks WHERE name = ?", c.QueryParam("task_name"))
		if err == sql.ErrNoRows {
			return echo.NewHTTPError(http.StatusBadRequest, "task not found")
		} else if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to get task: "+err.Error())
		}
		conditions = append(conditions, "task_id = ?")
		params = append(params, task.ID)
	}
	if c.QueryParam("user_name") != "" {
		user := User{}
		err := dbConn.GetContext(c.Request().Context(), &user, "SELECT * FROM users WHERE name = ?", c.QueryParam("user_name"))
		if err == sql.ErrNoRows {
			return echo.NewHTTPError(http.StatusBadRequest, "user not found")
		}
		conditions = append(conditions, "user_id = ?")
		params = append(params, user.ID)
	}
	if c.QueryParam("filter") != "" {
		conditions = append(conditions, "answer LIKE CONCAT('%', ?, '%')")
		params = append(params, c.QueryParam("filter"))
	}

	if username != "admin" || c.QueryParam("team_name") != "" {
		subconditions := "user_id = ?"
		params = append(params, team.LeaderID)
		if team.Member1ID != nulluserid {
			subconditions += " OR user_id = ?"
			params = append(params, team.Member1ID)
		}
		if team.Member2ID != nulluserid {
			subconditions += " OR user_id = ?"
			params = append(params, team.Member2ID)
		}
		conditions = append(conditions, "("+subconditions+")")
	}

	submissions := []Submission{}
	query := ""
	if len(conditions) > 0 {
		query = "SELECT * FROM submissions WHERE " + strings.Join(conditions, " AND ") + " ORDER BY submitted_at DESC"
	} else {
		query = "SELECT * FROM submissions ORDER BY submitted_at DESC"
	}
	if err := dbConn.SelectContext(c.Request().Context(), &submissions, query, params...); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get submissions: "+err.Error())
	}

	submissiondata := []SubmissionDetail{}
	for _, submission := range submissions {
		submissiondetail := SubmissionDetail{}
		task := Task{}
		if err := dbConn.GetContext(c.Request().Context(), &task, "SELECT * FROM tasks WHERE id = ?", submission.TaskID); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to get task: "+err.Error())
		}
		submissiondetail.TaskName = task.Name
		submissiondetail.TaskDisplayName = task.DisplayName

		answer := Answer{}
		err := dbConn.GetContext(c.Request().Context(), &answer, "SELECT * FROM answers WHERE task_id = ? AND answer = ?", task.ID, submission.Answer)
		if err == sql.ErrNoRows {
			submissiondetail.SubTaskName = ""
			submissiondetail.SubTaskDisplayName = ""
			submissiondetail.Score = 0
			submissiondetail.SubTaskMaxScore = 0
		} else if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to get answer: "+err.Error())
		} else {
			subtask := Subtask{}
			if err := dbConn.GetContext(c.Request().Context(), &subtask, "SELECT * FROM subtasks WHERE id = ?", answer.SubtaskID); err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to get subtask: "+err.Error())
			}
			submissiondetail.SubTaskName = subtask.Name
			submissiondetail.SubTaskDisplayName = subtask.DisplayName
			submissiondetail.Score = answer.Score

			if msc, ok := subtaskmaxscorecache.Load(subtask.ID); ok {
				submissiondetail.SubTaskMaxScore = msc.(int)
			} else {
				if err := dbConn.GetContext(c.Request().Context(), &submissiondetail.SubTaskMaxScore, "SELECT MAX(score) FROM answers WHERE subtask_id = ?", subtask.ID); err != nil {
					return echo.NewHTTPError(http.StatusInternalServerError, "failed to get subtask score: "+err.Error())
				}
				subtaskmaxscorecache.Store(subtask.ID, submissiondetail.SubTaskMaxScore)
			}
		}

		user := User{}
		if u, ok := usercache.Load(submission.UserID); ok {
			user = u.(User)
		} else {
			if err := dbConn.GetContext(c.Request().Context(), &user, "SELECT * FROM users WHERE id = ?", submission.UserID); err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to get user: "+err.Error())
			}
			usercache.Store(submission.UserID, user)
		}
		submissiondetail.UserName = user.Name
		submissiondetail.UserDisplayName = user.DisplayName
		submissiondetail.SubmittedAt = submission.SubmittedAt.Unix()
		submissiondetail.Answer = submission.Answer

		if c.QueryParam("subtask_name") == "" || c.QueryParam("subtask_name") == submissiondetail.SubTaskName {
			submissiondata = append(submissiondata, submissiondetail)
		}
	}

	page := 1 // 1-idx
	if c.QueryParam("page") != "" {
		p, err := strconv.Atoi(c.QueryParam("page"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "failed to parse page: "+err.Error())
		}
		page = p
	}
	if page < 1 {
		return echo.NewHTTPError(http.StatusBadRequest, "page must be positive")
	}
	start := (page - 1) * submissionsperpage
	end := start + submissionsperpage

	if end > len(submissiondata) {
		end = len(submissiondata)
	}
	if start > end {
		start = end
	}

	res := submissionresponse{
		Submissions:     submissiondata[start:end],
		SubmissionCount: len(submissiondata),
	}

	return c.JSON(http.StatusOK, res)
}
