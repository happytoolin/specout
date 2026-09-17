package main

import (
	"time"

	"github.com/happytoolin/specout"
)

// Category is components.schemas.Category.
type Category struct {
	ID   int64  `json:"id,omitempty" jsonschema:"format=int64,example=1"`
	Name string `json:"name,omitempty" jsonschema:"example=Dogs"`
}

// Tag is components.schemas.Tag.
type Tag struct {
	ID   int64  `json:"id,omitempty" jsonschema:"format=int64"`
	Name string `json:"name,omitempty"`
}

// Pet is components.schemas.Pet: required name + photoUrls, enum status.
type Pet struct {
	ID        int64    `json:"id,omitempty" jsonschema:"format=int64,example=10"`
	Name      string   `json:"name" jsonschema:"example=doggie"`
	Category  Category `json:"category,omitempty"`
	PhotoURLs []string `json:"photoUrls"`
	Tags      []Tag    `json:"tags,omitempty"`
	Status    string   `json:"status,omitempty" jsonschema:"description=pet status in the store,enum=available|pending|sold"`
}

// Order is components.schemas.Order.
type Order struct {
	ID       int64     `json:"id,omitempty" jsonschema:"format=int64,example=10"`
	PetID    int64     `json:"petId,omitempty" jsonschema:"format=int64,example=198772"`
	Quantity int32     `json:"quantity,omitempty" jsonschema:"format=int32,example=7"`
	ShipDate time.Time `json:"shipDate,omitempty"`
	Status   string    `json:"status,omitempty" jsonschema:"description=Order Status,example=approved,enum=placed|approved|delivered"`
	Complete bool      `json:"complete,omitempty"`
}

// User is components.schemas.User.
type User struct {
	ID         int64  `json:"id,omitempty" jsonschema:"format=int64,example=10"`
	Username   string `json:"username,omitempty" jsonschema:"example=theUser"`
	FirstName  string `json:"firstName,omitempty" jsonschema:"example=John"`
	LastName   string `json:"lastName,omitempty" jsonschema:"example=James"`
	Email      string `json:"email,omitempty" jsonschema:"example=john@email.com"`
	Password   string `json:"password,omitempty" jsonschema:"example=12345"`
	Phone      string `json:"phone,omitempty" jsonschema:"example=12345"`
	UserStatus int32  `json:"userStatus,omitempty" jsonschema:"description=User Status,format=int32,example=1"`
}

// ApiResponse is components.schemas.ApiResponse.
type ApiResponse struct {
	Code    int32  `json:"code,omitempty"`
	Type    string `json:"type,omitempty"`
	Message string `json:"message,omitempty"`
}

// A path:"name" field types the {name} placeholder: without one the
// placeholder is a plain string. Query and header fields become parameters.
type (
	findPetsByStatusReq struct {
		Status string `query:"status" jsonschema:"description=Status values that need to be considered for filter,default=available,enum=available|pending|sold"`
	}
	findPetsByTagsReq struct {
		Tags []string `query:"tags" jsonschema:"description=Tags to filter by"`
	}
	getPetReq struct {
		PetID int64 `path:"petId" jsonschema:"format=int64,description=ID of pet to return"`
	}
	updatePetFormReq struct {
		PetID  int64  `path:"petId" jsonschema:"format=int64,description=ID of pet that needs to be updated"`
		Name   string `query:"name,omitempty" jsonschema:"description=Name of pet that needs to be updated"`
		Status string `query:"status,omitempty" jsonschema:"description=Status of pet that needs to be updated"`
	}
	deletePetReq struct {
		PetID  int64  `path:"petId" jsonschema:"format=int64,description=Pet id to delete"`
		APIKey string `header:"api_key,omitempty"`
	}
	uploadImageReq struct {
		PetID              int64        `path:"petId" jsonschema:"format=int64,description=ID of pet to update"`
		AdditionalMetadata string       `query:"additionalMetadata,omitempty" jsonschema:"description=Additional Metadata"`
		File               specout.File `form:"file" jsonschema:"description=Upload image of the pet"`
	}
	getOrderReq struct {
		OrderID int64 `path:"orderId" jsonschema:"format=int64,description=ID of order that needs to be fetched"`
	}
	deleteOrderReq struct {
		OrderID int64 `path:"orderId" jsonschema:"format=int64,description=ID of the order that needs to be deleted"`
	}
	loginUserReq struct {
		Username string `query:"username,omitempty" jsonschema:"description=The user name for login"`
		Password string `query:"password,omitempty" jsonschema:"description=The password for login in clear text"`
	}
	getUserReq struct {
		Username string `path:"username" jsonschema:"description=The name that needs to be fetched. Use user1 for testing"`
	}
	deleteUserReq struct {
		Username string `path:"username" jsonschema:"description=The name that needs to be deleted"`
	}
)
