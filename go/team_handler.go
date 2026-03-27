package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"

	"github.com/labstack/echo-contrib/session"
	"github.com/labstack/echo/v4"
)

type Team struct {
	ID             int    `db:"id"`
	Name           string `db:"name"`
	DisplayName    string `db:"display_name"`
	LeaderID       int    `db:"leader_id"`
	Member1ID      int    `db:"member1_id"`
	Member2ID      int    `db:"member2_id"`
	Description    string `db:"description"`
	InvitationCode string `db:"invitation_code"`
}

type CreateTeamRequest struct {
	Name           string `json:"name"`
	DisplayName    string `json:"display_name"`
	leader_id      int
	Description    string `json:"description"`
	InvitationCode string
}

func generateInvitationCode() string {
	out, err := exec.Command("/bin/bash", "-c", "openssl rand -hex 8").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(string(out), "\n")
}

// POST /api/team/create
func createTeamHandler(c echo.Context) error {
	ctx := c.Request().Context()
	defer c.Request().Body.Close()

	if err := verifyUserSession(c); err != nil {
		return err
	}

	req := CreateTeamRequest{}

	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to decode the request body as json")
	}

	if req.Name == "" || req.DisplayName == "" || req.Description == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}

	req.InvitationCode = generateInvitationCode()

	sess, _ := session.Get(defaultSessionIDKey, c)
	username, _ := sess.Values[defaultSessionUserNameKey].(string)

	tx, err := dbConn.BeginTxx(ctx, nil)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to begin transaction: "+err.Error())
	}
	defer tx.Rollback()

	usr := User{}
	err = tx.GetContext(ctx, &usr, "SELECT * FROM users WHERE name = ?", username)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get user: "+err.Error())
	}
	req.leader_id = usr.ID

	team := Team{}

	err = tx.GetContext(ctx, &team, "SELECT * FROM teams WHERE name = ?", req.Name)
	if err == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "team already exists")
	} else if err != sql.ErrNoRows {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get team: "+err.Error())
	}

	err = tx.GetContext(ctx, &team, "SELECT * FROM teams WHERE leader_id = ? OR member1_id = ? OR member2_id = ?", usr.ID, usr.ID, usr.ID)
	if err == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "you have already joined team")
	} else if err != sql.ErrNoRows {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get team: "+err.Error())
	}

	if _, err = tx.ExecContext(ctx, "INSERT INTO teams (name, display_name, leader_id, member1_id, member2_id, description, invitation_code) VALUES (?, ?, ?, ?, ?, ?, ?)", req.Name, req.DisplayName, req.leader_id, nulluserid, nulluserid, req.Description, req.InvitationCode); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to insert team: "+err.Error())
	}

	if err = tx.Commit(); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to commit transaction: "+err.Error())
	}

	return c.NoContent(http.StatusCreated)
}

type JoinTeamRequest struct {
	TeamName       string `json:"team_name"`
	InvitationCode string `json:"invitation_code"`
}

type JoinTeamResponse struct {
	TeamName        string `json:"team_name"`
	TeamDisplayName string `json:"team_display_name"`
}

// POST /api/team/join
func joinTeamHandler(c echo.Context) error {
	ctx := c.Request().Context()
	defer c.Request().Body.Close()

	if err := verifyUserSession(c); err != nil {
		return err
	}

	req := JoinTeamRequest{}

	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to decode the request body as json")
	}

	tx, err := dbConn.BeginTxx(ctx, nil)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to begin transaction: "+err.Error())
	}
	defer tx.Rollback()

	team := Team{}
	err = tx.GetContext(ctx, &team, "SELECT * FROM teams WHERE name = ?", req.TeamName)
	if err == sql.ErrNoRows {
		return echo.NewHTTPError(http.StatusBadRequest, "team not found")
	} else if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get team: "+err.Error())
	}

	if team.InvitationCode != req.InvitationCode {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid invitation code")
	}

	sess, _ := session.Get(defaultSessionIDKey, c)
	username, _ := sess.Values[defaultSessionUserNameKey].(string)

	usr := User{}
	err = tx.GetContext(ctx, &usr, "SELECT * FROM users WHERE name = ?", username)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get user: "+err.Error())
	}

	err = tx.GetContext(ctx, &team, "SELECT * FROM teams WHERE leader_id = ? OR member1_id = ? OR member2_id = ?", usr.ID, usr.ID, usr.ID)
	if err == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "you have already joined team")
	} else if err != sql.ErrNoRows {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get team: "+err.Error())
	}

	if team.Member1ID == nulluserid {
		if _, err := tx.ExecContext(ctx, "UPDATE teams SET member1_id = ? WHERE name = ?", usr.ID, req.TeamName); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to update team: "+err.Error())
		}
	} else if team.Member2ID == nulluserid {
		if _, err := tx.ExecContext(ctx, "UPDATE teams SET member2_id = ? WHERE name = ?", usr.ID, req.TeamName); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to update team: "+err.Error())
		}
	} else {
		return echo.NewHTTPError(http.StatusBadRequest, "team is full")
	}

	if err = tx.Commit(); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to commit transaction: "+err.Error())
	}

	return c.JSON(http.StatusCreated, JoinTeamResponse{
		TeamName:        team.Name,
		TeamDisplayName: team.DisplayName,
	})
}

type TeamResponse struct {
	Name               string `json:"name"`
	DisplayName        string `json:"display_name"`
	LeaderName         string `json:"leader_name"`
	LeaderDisplayName  string `json:"leader_display_name"`
	Member1Name        string `json:"member1_name,omitempty"`
	Member1DisplayName string `json:"member1_display_name,omitempty"`
	Member2Name        string `json:"member2_name,omitempty"`
	Member2DisplayName string `json:"member2_display_name,omitempty"`
	Description        string `json:"description"`
	SubmissionCount    int    `json:"submission_count"`
	InvitationCode     string `json:"invitation_code,omitempty"`
}

// GET /api/team/:teamname
func getTeamHandler(c echo.Context) error {
	teamname := c.Param("teamname")

	tx, err := dbConn.BeginTxx(c.Request().Context(), nil)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to begin transaction: "+err.Error())
	}
	defer tx.Rollback()

	team := Team{}

	err = tx.GetContext(c.Request().Context(), &team, "SELECT * FROM teams WHERE name = ?", teamname)

	if err == sql.ErrNoRows {
		return echo.NewHTTPError(http.StatusNotFound, "team not found")
	} else if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get team: "+err.Error())
	}

	res := TeamResponse{
		Name:        team.Name,
		DisplayName: team.DisplayName,
		Description: team.Description,
	}

	leader := User{}
	err = tx.GetContext(c.Request().Context(), &leader, "SELECT * FROM users WHERE id = ?", team.LeaderID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get leader: "+err.Error())
	}
	res.LeaderName = leader.Name
	res.LeaderDisplayName = leader.DisplayName

	if err = tx.GetContext(c.Request().Context(), &res.SubmissionCount, "SELECT COUNT(*) FROM submissions WHERE user_id = ?", leader.ID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get submission count: "+err.Error())
	}

	if team.Member1ID != nulluserid {
		member1 := User{}
		err = tx.GetContext(c.Request().Context(), &member1, "SELECT * FROM users WHERE id = ?", team.Member1ID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to get member1: "+err.Error())
		}
		res.Member1Name = member1.Name
		res.Member1DisplayName = member1.DisplayName

		membersubmissioncount := 0
		if err = tx.GetContext(c.Request().Context(), &membersubmissioncount, "SELECT COUNT(*) FROM submissions WHERE user_id = ?", member1.ID); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to get submission count: "+err.Error())
		}
		res.SubmissionCount += membersubmissioncount
	}

	if team.Member2ID != nulluserid {
		member2 := User{}
		err = tx.GetContext(c.Request().Context(), &member2, "SELECT * FROM users WHERE id = ?", team.Member2ID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to get member2: "+err.Error())
		}
		res.Member2Name = member2.Name
		res.Member2DisplayName = member2.DisplayName

		membersubmissioncount := 0
		if err = tx.GetContext(c.Request().Context(), &membersubmissioncount, "SELECT COUNT(*) FROM submissions WHERE user_id = ?", member2.ID); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to get submission count: "+err.Error())
		}
		res.SubmissionCount += membersubmissioncount
	}

	sess, err := session.Get(defaultSessionIDKey, c)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get session: "+err.Error())
	}
	username, ok := sess.Values[defaultSessionUserNameKey].(string)
	if ok && username == res.LeaderName {
		res.InvitationCode = team.InvitationCode
	}

	if err = tx.Commit(); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to commit transaction: "+err.Error())
	}

	return c.JSON(http.StatusOK, res)
}
