package server

import (
	"diffractllm/internal/core"
	"diffractllm/internal/dbstore"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const adminMaxBody = 1 << 20

func adminErr(c *gin.Context, code int, msg string) {
	c.AbortWithStatusJSON(code, gin.H{"error": msg})
}

// --------- Budget handlers -------------

func (ds *DiffractLLMServer) createBudget(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, adminMaxBody)
	var payload core.Budget
	if err := c.ShouldBindJSON(&payload); err != nil {
		adminErr(c, http.StatusBadRequest, err.Error())
		return
	}

	row, err := ds.dbStore.CreateBudget(payload)
	if err != nil {
		ds.logger.Error("create budget", zap.Error(err))
		adminErr(c, http.StatusBadRequest, err.Error())
		return
	}
	ds.governance.BudgetCache.UpsertBudget(row.ToCore())
	c.JSON(http.StatusCreated, row.ToCore())
}

func (ds *DiffractLLMServer) updateBudget(c *gin.Context) {
	id := c.Param("id")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, adminMaxBody)

	var payload core.Budget
	if err := c.ShouldBind(&payload); err != nil {
		adminErr(c, http.StatusBadRequest, err.Error())
		return
	}

	row, err := ds.dbStore.UpdateBudget(id, payload)
	if err != nil {
		ds.logger.Error("update budget", zap.Error(err))
		adminErr(c, http.StatusBadRequest, err.Error())
		return
	}
	ds.governance.BudgetCache.UpsertBudget(row.ToCore())
	c.JSON(http.StatusOK, row.ToCore())
}

func (ds *DiffractLLMServer) deleteBudget(c *gin.Context) {
	id := c.Param("id")
	if err := ds.dbStore.DeleteBudget(id); err != nil {
		adminErr(c, http.StatusConflict, err.Error())
		return
	}
	ds.governance.BudgetCache.DeleteBudget(id)
	c.Status(http.StatusNoContent)
}

func (ds *DiffractLLMServer) listBudgets(c *gin.Context) {
	rows, err := ds.dbStore.ListBudgets()
	if err != nil {
		adminErr(c, http.StatusInternalServerError, "listing budgets")
		return
	}
	out := make([]*core.Budget, len(rows))
	for i := range rows {
		out[i] = rows[i].ToCore()
	}
	c.JSON(http.StatusOK, out)
}

// ----------- virtual key ----------------

func (ds *DiffractLLMServer) createVirtualKey(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, adminMaxBody)

	var payload core.VirtualKeyRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		adminErr(c, http.StatusBadRequest, err.Error())
		return
	}

	result, apiKey, err := ds.dbStore.CreateVirtualKeyTx(&payload)
	if err != nil {
		adminErr(c, http.StatusBadRequest, err.Error())
		return
	}

	vk, err := result.VirtualKey.ToCore()
	if err != nil {
		ds.logger.Error("virtual key to core", zap.Error(err))
		adminErr(c, http.StatusInternalServerError, "stored, not yet live")
		return
	}
	ds.governance.KeyCache.UpsertVirtualKey(vk)
	resp, _ := result.VirtualKey.ToResponse()
	c.JSON(http.StatusCreated, gin.H{"key": apiKey, "virtual_key": resp})
}

func (ds *DiffractLLMServer) updateVirtualKeyRouting(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, adminMaxBody)

	var payload dbstore.UpdateVirtualKeyRoutingRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		adminErr(c, http.StatusBadRequest, err.Error())
		return
	}
	row, err := ds.dbStore.UpdateVirtualKeyRouting(c.Param("id"), payload)
	if err != nil {
		adminErr(c, http.StatusBadRequest, err.Error())
		return
	}
	vk, err := row.ToCore()
	if err != nil {
		adminErr(c, http.StatusInternalServerError, "stored, not yet live")
		return
	}
	ds.governance.KeyCache.UpsertVirtualKey(vk)

	resp, _ := row.ToResponse()
	c.JSON(http.StatusOK, resp)
}

func (ds *DiffractLLMServer) revokeVirtualKey(c *gin.Context) {
	id := c.Param("id")
	if err := ds.dbStore.RevokeVirtualKey(id); err != nil {
		adminErr(c, http.StatusNotFound, "virtual key not found")
		return
	}
	ds.governance.KeyCache.DeleteVirtualKeyByID(id)
	c.Status(http.StatusNoContent)
}

func (ds *DiffractLLMServer) listVirtualKeys(c *gin.Context) {
	rows, err := ds.dbStore.ListVirtualKeysWithoutKeys()
	if err != nil {
		adminErr(c, http.StatusInternalServerError, "listing virtual keys")
		return
	}
	out := make([]core.VirtualKeyResponse, 0, len(rows))
	for i := range rows {
		resp, err := rows[i].ToResponse()
		if err != nil {
			continue
		}
		out = append(out, resp)
	}
	c.JSON(http.StatusOK, out)
}

func (ds *DiffractLLMServer) rotateVirtualKey(c *gin.Context) {
	id := c.Param("id")
	row, apiKey, err := ds.dbStore.RotateVirtualKey(id)
	if err != nil {
		adminErr(c, http.StatusBadRequest, err.Error())
		return
	}
	vk, err := row.ToCore()
	if err != nil {
		adminErr(c, http.StatusInternalServerError, "stored, not yet live")
		return
	}
	ds.governance.KeyCache.DeleteVirtualKeyByID(id)
	ds.governance.KeyCache.UpsertVirtualKey(vk)
	resp, _ := row.ToResponse()
	c.JSON(http.StatusOK, gin.H{"key": apiKey, "virtual_key": resp})
}
