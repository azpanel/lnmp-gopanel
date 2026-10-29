package server

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/azpanel/lnmp-gopanel/internal/control"
	"github.com/gin-gonic/gin"
)

func managementRepairFailure(c *gin.Context, err error) {
	status := 409
	if errors.Is(err, control.ErrUnsupported) {
		status = 400
	}
	fail(c, status, err.Error())
}

func (s *Server) managementRepairPreview(c *gin.Context) {
	ctx, done := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer done()
	plan, err := s.Services.ManagementRepairPreview(ctx, c.Param("name"))
	if err != nil {
		managementRepairFailure(c, err)
		return
	}
	c.JSON(200, plan)
}

func (s *Server) managementRepair(c *gin.Context) {
	var r control.ManagementRepairRequest
	d := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 1024))
	d.DisallowUnknownFields()
	decodeErr := d.Decode(&r)
	_, revisionErr := hex.DecodeString(r.Revision)
	var extra any
	if decodeErr != nil || len(r.Revision) != 64 || revisionErr != nil || d.Decode(&extra) != io.EOF {
		fail(c, 400, "修复预览标识无效，请重新预览")
		return
	}
	result, err := s.Services.RepairManagement(c.Request.Context(), c.Param("name"), r)
	if err != nil {
		managementRepairFailure(c, err)
		return
	}
	c.JSON(200, result)
}
