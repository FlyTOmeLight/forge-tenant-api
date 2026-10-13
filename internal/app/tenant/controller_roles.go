package tenant

import (
	"net/http"
)

func (a *Controller) roles(w http.ResponseWriter, r *http.Request, u User) {
	JSON(w, 200, Object{"items": roleDefinitions()})
}
