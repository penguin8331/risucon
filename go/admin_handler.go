package main

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/labstack/echo-contrib/session"
	"github.com/labstack/echo/v4"
)

type AnswerRequest struct {
	Answer string `json:"answer"`
	Score  int    `json:"score"`
}
type SubtaskRequest struct {
	Name        string          `json:"name"`
	DisplayName string          `json:"display_name"`
	Statement   string          `json:"statement"`
	Answers     []AnswerRequest `json:"answers"`
}
type CreateTaskRequest struct {
	Name            string           `json:"name"`
	DisplayName     string           `json:"display_name"`
	Statement       string           `json:"statement"`
	SubmissionLimit int              `json:"submission_limit"`
	Subtasks        []SubtaskRequest `json:"subtasks"`
}

// POST /api/admin/createtask
func createTaskHandler(c echo.Context) error {
	ctx := c.Request().Context()
	defer c.Request().Body.Close()

	if err := verifyUserSession(c); err != nil {
		return err
	}

	sess, _ := session.Get(defaultSessionIDKey, c)
	username, _ := sess.Values[defaultSessionUserNameKey].(string)

	if username != "admin" {
		return echo.NewHTTPError(http.StatusUnauthorized, "not admin")
	}

	req := CreateTaskRequest{}
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to decode the request body as json")
	}

	tx, err := dbConn.BeginTxx(ctx, nil)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to begin transaction: "+err.Error())
	}
	defer tx.Rollback()

	task := Task{}
	err = tx.GetContext(ctx, &task, "SELECT * FROM tasks WHERE name = ?", req.Name)
	if err == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "task already exists")
	} else if err != sql.ErrNoRows {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get task: "+err.Error())
	}
	
	if _, err := tx.ExecContext(ctx, "INSERT INTO tasks (name, display_name, statement, submission_limit) VALUES (?, ?, ?, ?)", req.Name, req.DisplayName, req.Statement, req.SubmissionLimit); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to insert task: "+err.Error())
	}
	var taskID int
	err = tx.GetContext(ctx, &taskID, "SELECT id FROM tasks WHERE name = ?", req.Name)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get taskID: "+err.Error())
	}

	for _, subtask := range req.Subtasks {
		subtasktmp := Subtask{}
		err = tx.GetContext(ctx, &subtasktmp, "SELECT * FROM subtasks WHERE task_id = ? AND name = ?", taskID, req.Name)
		if err == nil {
			return echo.NewHTTPError(http.StatusBadRequest, "subtask already exists")
		} else if err != sql.ErrNoRows {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to get subtask: "+err.Error())
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO subtasks (name, display_name, task_id, statement) VALUES (?, ?, ?, ?)", subtask.Name, subtask.DisplayName, taskID, subtask.Statement); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to insert subtask: "+err.Error())
		}
		var subtaskID int
		err = tx.GetContext(ctx, &subtaskID, "SELECT id FROM subtasks WHERE task_id = ? AND name = ?", taskID, subtask.Name)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to get subtaskID: "+err.Error())
		}
		for _, answer := range subtask.Answers {
			if _, err := tx.ExecContext(ctx, "INSERT INTO answers (task_id, subtask_id, answer, score) VALUES (?, ?, ?, ?)", taskID, subtaskID, answer.Answer, answer.Score); err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to insert answer: "+err.Error())
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to commit transaction: "+err.Error())
	}

	return c.NoContent(http.StatusCreated)
}
