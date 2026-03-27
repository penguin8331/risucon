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

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo-contrib/session"
	"github.com/labstack/echo/v4"
)

var (
	// Subtask は update がないのでキャッシュしておく
	// メモ: initializeHandler でキャッシュを消すのを忘れずに
	subtaskcache = sync.Map{}
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
}

type TaskAbstract struct {
	Name            string `json:"name"`
	DisplayName     string `json:"display_name"`
	MaxScore        int    `json:"max_score"`
	Score           int    `json:"score,omitempty"`
	SubmissionLimit int    `json:"submission_limit,omitempty"`
	SubmissionCount int    `json:"submission_count,omitempty"`
}

func gettaskabstarcts(ctx context.Context, tx *sqlx.Tx, c echo.Context) ([]TaskAbstract, error) {
	tasks := []Task{}
	if err := tx.SelectContext(ctx, &tasks, "SELECT * FROM tasks ORDER BY name"); err != nil {
		return []TaskAbstract{}, err
	}
	res := []TaskAbstract{}
	for _, task := range tasks {
		maxscore := 0
		subtasks := []Subtask{}
		if err := tx.SelectContext(ctx, &subtasks, "SELECT * FROM subtasks WHERE task_id = ?", task.ID); err != nil {
			return []TaskAbstract{}, err
		}
		for _, subtask := range subtasks {
			maxscore_for_subtask := 0
			if err := tx.GetContext(ctx, &maxscore_for_subtask, "SELECT MAX(score) FROM answers WHERE subtask_id = ?", subtask.ID); err != nil {
				return []TaskAbstract{}, err
			}
			maxscore += maxscore_for_subtask
		}
		submissioncount := 0
		score := 0
		if err := verifyUserSession(c); err == nil {
			sess, _ := session.Get(defaultSessionIDKey, c)
			username, _ := sess.Values[defaultSessionUserNameKey].(string)
			user := User{}
			if err := tx.GetContext(c.Request().Context(), &user, "SELECT * FROM users WHERE name = ?", username); err != nil {
				return []TaskAbstract{}, err
			}
			team := Team{}
			err := tx.GetContext(c.Request().Context(), &team, "SELECT * FROM teams WHERE leader_id = ? OR member1_id = ? OR member2_id = ?", user.ID, user.ID, user.ID)
			if err == nil {
				err := tx.GetContext(c.Request().Context(), &submissioncount, "SELECT COUNT(*) FROM submissions WHERE task_id = ? AND user_id = ?", task.ID, team.LeaderID)
				if err != nil {
					return []TaskAbstract{}, err
				}
				if team.Member1ID != nulluserid {
					cnt := 0
					err := tx.GetContext(c.Request().Context(), &cnt, "SELECT COUNT(*) FROM submissions WHERE task_id = ? AND user_id = ?", task.ID, team.Member1ID)
					if err != nil {
						return []TaskAbstract{}, err
					}
					submissioncount += cnt
				}
				if team.Member2ID != nulluserid {
					cnt := 0
					err := tx.GetContext(c.Request().Context(), &cnt, "SELECT COUNT(*) FROM submissions WHERE task_id = ? AND user_id = ?", task.ID, team.Member2ID)
					if err != nil {
						return []TaskAbstract{}, err
					}
					submissioncount += cnt
				}
				for _, subtask := range subtasks {
					score_for_subtask := 0
					leaderscore := 0
					if err := tx.GetContext(ctx, &leaderscore, "SELECT COALESCE(MAX(score),0) FROM answers WHERE subtask_id = ? AND EXISTS (SELECT * FROM submissions WHERE task_id = ? AND user_id = ? AND submissions.answer = answers.answer)", subtask.ID, task.ID, team.LeaderID); err != nil {
						return []TaskAbstract{}, err
					}
					if score_for_subtask < leaderscore {
						score_for_subtask = leaderscore
					}
					if team.Member1ID != nulluserid {
						member1score := 0
						if err := tx.GetContext(ctx, &member1score, "SELECT COALESCE(MAX(score),0) FROM answers WHERE subtask_id = ? AND EXISTS (SELECT * FROM submissions WHERE task_id = ? AND user_id = ? AND submissions.answer = answers.answer)", subtask.ID, task.ID, team.Member1ID); err != nil {
							return []TaskAbstract{}, err
						}
						if score_for_subtask < member1score {
							score_for_subtask = member1score
						}
					}
					if team.Member2ID != nulluserid {
						member2score := 0
						if err := tx.GetContext(ctx, &member2score, "SELECT COALESCE(MAX(score),0) FROM answers WHERE subtask_id = ? AND EXISTS (SELECT * FROM submissions WHERE task_id = ? AND user_id = ? AND submissions.answer = answers.answer)", subtask.ID, task.ID, team.Member2ID); err != nil {
							return []TaskAbstract{}, err
						}
						if score_for_subtask < member2score {
							score_for_subtask = member2score
						}
					}
					score += score_for_subtask
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

	tx, err := dbConn.BeginTxx(ctx, nil)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to begin transaction: "+err.Error())
	}
	defer tx.Rollback()

	taskabstarcts, err := gettaskabstarcts(ctx, tx, c)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get taskabstarcts: "+err.Error())
	}

	if err := tx.Commit(); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to commit transaction: "+err.Error())
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

func getstandings(ctx context.Context, tx *sqlx.Tx) (Standings, error) {
	standings := Standings{}

	tasks := []Task{}
	if err := tx.SelectContext(ctx, &tasks, "SELECT * FROM tasks ORDER BY name"); err != nil {
		return Standings{}, err
	}
	for _, task := range tasks {
		subtasks := []Subtask{}
		if err := tx.SelectContext(ctx, &subtasks, "SELECT * FROM subtasks WHERE task_id = ?", task.ID); err != nil {
			return Standings{}, err
		}
		maxscore := 0
		for _, subtask := range subtasks {
			subtaskmaxscore := 0
			if err := tx.GetContext(ctx, &subtaskmaxscore, "SELECT MAX(score) FROM answers WHERE subtask_id = ?", subtask.ID); err != nil {
				return Standings{}, err
			}
			maxscore += subtaskmaxscore
		}
		standings.TasksData = append(standings.TasksData, TaskAbstract{
			Name:        task.Name,
			DisplayName: task.DisplayName,
			MaxScore:    maxscore,
		})
	}

	teams := []Team{}
	if err := tx.SelectContext(ctx, &teams, "SELECT * FROM teams ORDER BY name"); err != nil {
		return Standings{}, err
	}
	for _, team := range teams {
		teamstandings := TeamsStandings{}
		teamstandings.TeamName = team.Name
		teamstandings.TeamDisplayName = team.DisplayName
		teamstandings.TotalScore = 0

		leader := User{}
		if err := tx.GetContext(ctx, &leader, "SELECT * FROM users WHERE id = ?", team.LeaderID); err != nil {
			return Standings{}, err
		}
		teamstandings.LeaderName = leader.Name
		teamstandings.LeaderDisplayName = leader.DisplayName
		if team.Member1ID != nulluserid {
			member1 := User{}
			if err := tx.GetContext(ctx, &member1, "SELECT * FROM users WHERE id = ?", team.Member1ID); err != nil {
				return Standings{}, err
			}
			teamstandings.Member1Name = member1.Name
			teamstandings.Member1DisplayName = member1.DisplayName
		}
		if team.Member2ID != nulluserid {
			member2 := User{}
			if err := tx.GetContext(ctx, &member2, "SELECT * FROM users WHERE id = ?", team.Member2ID); err != nil {
				return Standings{}, err
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
			if err := tx.SelectContext(ctx, &subtasks, "SELECT * FROM subtasks WHERE task_id = ?", task.ID); err != nil {
				return Standings{}, err
			}
			submissioncount := 0
			if err := tx.GetContext(ctx, &submissioncount, "SELECT COUNT(*) FROM submissions WHERE task_id = ? AND user_id = ?", task.ID, team.LeaderID); err != nil {
				return Standings{}, err
			}
			if submissioncount > 0 {
				taskscoringdata.HasSubmitted = true
			}
			if team.Member1ID != nulluserid {
				if err := tx.GetContext(ctx, &submissioncount, "SELECT COUNT(*) FROM submissions WHERE task_id = ? AND user_id = ?", task.ID, team.Member1ID); err != nil {
					return Standings{}, err
				}
				if submissioncount > 0 {
					taskscoringdata.HasSubmitted = true
				}
			}
			if team.Member2ID != nulluserid {
				if err := tx.GetContext(ctx, &submissioncount, "SELECT COUNT(*) FROM submissions WHERE task_id = ? AND user_id = ?", task.ID, team.Member2ID); err != nil {
					return Standings{}, err
				}
				if submissioncount > 0 {
					taskscoringdata.HasSubmitted = true
				}
			}

			for _, subtask := range subtasks {
				subtaskscore := 0

				leaderscore := 0
				if err := tx.GetContext(ctx, &leaderscore, "SELECT COALESCE(MAX(score),0) FROM answers WHERE subtask_id = ? AND EXISTS (SELECT * FROM submissions WHERE task_id = ? AND user_id = ? AND submissions.answer = answers.answer)", subtask.ID, task.ID, team.LeaderID); err != nil {
					return Standings{}, err
				}
				if subtaskscore < leaderscore {
					subtaskscore = leaderscore
				}

				if team.Member1ID != nulluserid {
					member1score := 0
					if err := tx.GetContext(ctx, &member1score, "SELECT COALESCE(MAX(score),0) FROM answers WHERE subtask_id = ? AND EXISTS (SELECT * FROM submissions WHERE task_id = ? AND user_id = ? AND submissions.answer = answers.answer)", subtask.ID, task.ID, team.Member1ID); err != nil {
						return Standings{}, err
					}
					if subtaskscore < member1score {
						subtaskscore = member1score
					}
				}
				if team.Member2ID != nulluserid {
					member2score := 0
					if err := tx.GetContext(ctx, &member2score, "SELECT COALESCE(MAX(score),0) FROM answers WHERE subtask_id = ? AND EXISTS (SELECT * FROM submissions WHERE task_id = ? AND user_id = ? AND submissions.answer = answers.answer)", subtask.ID, task.ID, team.Member2ID); err != nil {
						return Standings{}, err
					}
					if subtaskscore < member2score {
						subtaskscore = member2score
					}
				}
				taskscoringdata.Score += subtaskscore
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

	tx, err := dbConn.BeginTxx(ctx, nil)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to begin transaction: "+err.Error())
	}
	defer tx.Rollback()

	standings, err := getstandings(ctx, tx)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get standings: "+err.Error())
	}

	if err := tx.Commit(); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to commit transaction: "+err.Error())
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

	tx, err := dbConn.BeginTxx(c.Request().Context(), nil)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to begin transaction: "+err.Error())
	}
	defer tx.Rollback()

	task := Task{}

	err = tx.GetContext(c.Request().Context(), &task, "SELECT * FROM tasks WHERE name = ?", taskname)

	if err == sql.ErrNoRows {
		return echo.NewHTTPError(http.StatusNotFound, "task not found")
	} else if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get task: "+err.Error())
	}

	subtasks := []Subtask{}

	if cache_data, ok := subtaskcache.Load(task.ID) ; ok {
		// データがキャッシュされているので、それを読み込む
		subtasks = cache_data.([]Subtask)
	}

	if err := tx.SelectContext(c.Request().Context(), &subtasks, "SELECT * FROM subtasks WHERE task_id = ?", task.ID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get subtasks: "+err.Error())
	}

	// キャッシュにデータを保存
	subtaskcache.Store(task.ID, subtasks)

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
		if err := tx.GetContext(c.Request().Context(), &subtaskdetail.MaxScore, "SELECT MAX(score) FROM answers WHERE subtask_id = ?", subtask.ID); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to get subtask score: "+err.Error())
		}
		res.Subtasks = append(res.Subtasks, subtaskdetail)
		res.MaxScore += subtaskdetail.MaxScore
	}

	if err := verifyUserSession(c); err == nil {
		sess, _ := session.Get(defaultSessionIDKey, c)
		username, _ := sess.Values[defaultSessionUserNameKey].(string)
		user := User{}
		if err := tx.GetContext(c.Request().Context(), &user, "SELECT * FROM users WHERE name = ?", username); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to get user: "+err.Error())
		}
		team := Team{}
		err := tx.GetContext(c.Request().Context(), &team, "SELECT * FROM teams WHERE leader_id = ? OR member1_id = ? OR member2_id = ?", user.ID, user.ID, user.ID)
		if err == nil {
			err := tx.GetContext(c.Request().Context(), &res.SubmissionCount, "SELECT COUNT(*) FROM submissions WHERE task_id = ? AND user_id = ?", task.ID, team.LeaderID)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to get submission count: "+err.Error())
			}
			if team.Member1ID != nulluserid {
				cnt := 0
				err := tx.GetContext(c.Request().Context(), &cnt, "SELECT COUNT(*) FROM submissions WHERE task_id = ? AND user_id = ?", task.ID, team.Member1ID)
				if err != nil {
					return echo.NewHTTPError(http.StatusInternalServerError, "failed to get submission count: "+err.Error())
				}
				res.SubmissionCount += cnt
			}
			if team.Member2ID != nulluserid {
				cnt := 0
				err := tx.GetContext(c.Request().Context(), &cnt, "SELECT COUNT(*) FROM submissions WHERE task_id = ? AND user_id = ?", task.ID, team.Member2ID)
				if err != nil {
					return echo.NewHTTPError(http.StatusInternalServerError, "failed to get submission count: "+err.Error())
				}
				res.SubmissionCount += cnt
			}

			for i, subtask := range subtasks {
				subtaskscore := 0
				leaderscore := 0
				if err := tx.GetContext(c.Request().Context(), &leaderscore, "SELECT COALESCE(MAX(score),0) FROM answers WHERE subtask_id = ? AND EXISTS (SELECT * FROM submissions WHERE task_id = ? AND user_id = ? AND submissions.answer = answers.answer)", subtask.ID, task.ID, team.LeaderID); err != nil {
					return echo.NewHTTPError(http.StatusInternalServerError, "failed to get subtask score: "+err.Error())
				}
				if subtaskscore < leaderscore {
					subtaskscore = leaderscore
				}
				if team.Member1ID != nulluserid {
					member1score := 0
					if err := tx.GetContext(c.Request().Context(), &member1score, "SELECT COALESCE(MAX(score),0) FROM answers WHERE subtask_id = ? AND EXISTS (SELECT * FROM submissions WHERE task_id = ? AND user_id = ? AND submissions.answer = answers.answer)", subtask.ID, task.ID, team.Member1ID); err != nil {
						return echo.NewHTTPError(http.StatusInternalServerError, "failed to get subtask score: "+err.Error())
					}
					if subtaskscore < member1score {
						subtaskscore = member1score
					}
				}
				if team.Member2ID != nulluserid {
					member2score := 0
					if err := tx.GetContext(c.Request().Context(), &member2score, "SELECT COALESCE(MAX(score),0) FROM answers WHERE subtask_id = ? AND EXISTS (SELECT * FROM submissions WHERE task_id = ? AND user_id = ? AND submissions.answer = answers.answer)", subtask.ID, task.ID, team.Member2ID); err != nil {
						return echo.NewHTTPError(http.StatusInternalServerError, "failed to get subtask score: "+err.Error())
					}
					if subtaskscore < member2score {
						subtaskscore = member2score
					}
				}
				res.Subtasks[i].Score = subtaskscore
				res.Score += subtaskscore
			}
		} else if err != sql.ErrNoRows {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to get team: "+err.Error())
		}
	}

	if err := tx.Commit(); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to commit transaction: "+err.Error())
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
	if err := tx.GetContext(c.Request().Context(), &submissionscount, "SELECT COUNT(*) FROM submissions WHERE task_id = ? AND user_id = ?", task.ID, team.LeaderID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get submissions count: "+err.Error())
	}

	if team.Member1ID != nulluserid {
		cnt := 0
		if err := tx.GetContext(c.Request().Context(), &cnt, "SELECT COUNT(*) FROM submissions WHERE task_id = ? AND user_id = ?", task.ID, team.Member1ID); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to get submissions count: "+err.Error())
		}
		submissionscount += cnt
	}

	if team.Member2ID != nulluserid {
		cnt := 0
		if err := tx.GetContext(c.Request().Context(), &cnt, "SELECT COUNT(*) FROM submissions WHERE task_id = ? AND user_id = ?", task.ID, team.Member2ID); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to get submissions count: "+err.Error())
		}
		submissionscount += cnt
	}

	if submissionscount >= task.SubmissionLimit {
		return echo.NewHTTPError(http.StatusBadRequest, "submission limit exceeded")
	}

	timestamp := time.Unix(req.Timestamp, 0)

	if _, err = tx.ExecContext(ctx, "INSERT INTO submissions (task_id, user_id, submitted_at, answer) VALUES (?, ?, ?, ?)", task.ID, user.ID, timestamp, req.Answer); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to insert submission: "+err.Error())
	}

	res := SubmitResponse{}

	// デフォルトではこれを返す。答えが有効な場合は更新される。
	res.IsScored = false
	res.Score = 0
	res.RemainingSubmissions = task.SubmissionLimit - submissionscount - 1

	subtasks := []Subtask{}
	if err := tx.SelectContext(c.Request().Context(), &subtasks, "SELECT * FROM subtasks WHERE task_id = ?", task.ID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get subtasks: "+err.Error())
	}
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
			}
		}
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
	if username != "admin" {
		err = tx.GetContext(c.Request().Context(), &team, "SELECT * FROM teams WHERE leader_id = ? OR member1_id = ? OR member2_id = ?", user.ID, user.ID, user.ID)
		if err == sql.ErrNoRows {
			return echo.NewHTTPError(http.StatusBadRequest, "you have not joined team")
		} else if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to get team: "+err.Error())
		}
	} else if c.QueryParam("team_name") != "" {
		err = tx.GetContext(c.Request().Context(), &team, "SELECT * FROM teams WHERE name = ?", c.QueryParam("team_name"))
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
		err = tx.GetContext(c.Request().Context(), &task, "SELECT * FROM tasks WHERE name = ?", c.QueryParam("task_name"))
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
		err = tx.GetContext(c.Request().Context(), &user, "SELECT * FROM users WHERE name = ?", c.QueryParam("user_name"))
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
	if err := tx.SelectContext(c.Request().Context(), &submissions, query, params...); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get submissions: "+err.Error())
	}

	submissiondata := []SubmissionDetail{}
	for _, submission := range submissions {
		submissiondetail := SubmissionDetail{}
		task := Task{}
		if err := tx.GetContext(c.Request().Context(), &task, "SELECT * FROM tasks WHERE id = ?", submission.TaskID); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to get task: "+err.Error())
		}
		submissiondetail.TaskName = task.Name
		submissiondetail.TaskDisplayName = task.DisplayName

		answer := Answer{}
		err = tx.GetContext(c.Request().Context(), &answer, "SELECT * FROM answers WHERE task_id = ? AND answer = ?", task.ID, submission.Answer)
		if err == sql.ErrNoRows {
			submissiondetail.SubTaskName = ""
			submissiondetail.SubTaskDisplayName = ""
			submissiondetail.Score = 0
			submissiondetail.SubTaskMaxScore = 0
		} else if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to get answer: "+err.Error())
		} else {
			subtask := Subtask{}
			if err := tx.GetContext(c.Request().Context(), &subtask, "SELECT * FROM subtasks WHERE id = ?", answer.SubtaskID); err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to get subtask: "+err.Error())
			}
			submissiondetail.SubTaskName = subtask.Name
			submissiondetail.SubTaskDisplayName = subtask.DisplayName
			submissiondetail.Score = answer.Score

			if err := tx.GetContext(c.Request().Context(), &submissiondetail.SubTaskMaxScore, "SELECT MAX(score) FROM answers WHERE subtask_id = ?", subtask.ID); err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to get subtask score: "+err.Error())
			}
		}

		user := User{}
		if err := tx.GetContext(c.Request().Context(), &user, "SELECT * FROM users WHERE id = ?", submission.UserID); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to get user: "+err.Error())
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
		page, err = strconv.Atoi(c.QueryParam("page"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "failed to parse page: "+err.Error())
		}
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

	if err := tx.Commit(); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to commit transaction: "+err.Error())
	}

	return c.JSON(http.StatusOK, res)
}
