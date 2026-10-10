package main

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/acme/dispatchly/models"
)

type contactReq struct {
	models.Contact
	Lists []int `json:"lists"`
}

// UpdateContact saves a contact and its list memberships.
func (a *App) UpdateContact(c *Ctx) error {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return c.Error(http.StatusBadRequest, "invalid contact id")
	}
	var req contactReq
	if err := json.NewDecoder(c.R.Body).Decode(&req); err != nil {
		return c.Error(http.StatusBadRequest, "invalid contact")
	}
	out, err := a.core.UpdateContactWithLists(id, req.Contact, req.Lists)
	if err != nil {
		return c.Error(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, out)
}
