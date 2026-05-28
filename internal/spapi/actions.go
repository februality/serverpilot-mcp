package spapi

// ActionsAPI wraps the /actions endpoint. No caching — action status flips
// from "open" to "success"/"error" asynchronously.
type ActionsAPI struct {
	client *Client
}

func NewActionsAPI(client *Client) *ActionsAPI {
	return &ActionsAPI{client: client}
}

func (a *ActionsAPI) Get(id string) (*SPAction, error) {
	var resp spSingleResponse[SPAction]
	if err := a.client.Get("/actions/"+id, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}
