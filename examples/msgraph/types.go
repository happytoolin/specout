package main

// ODataError is microsoft.graph.ODataErrors.ODataError: the 4XX/5XX envelope.
type ODataError struct {
	Error MainError `json:"error"`
}

type MainError struct {
	Code       string        `json:"code"`
	Message    string        `json:"message"`
	Target     string        `json:"target,omitempty"`
	Details    []ErrorDetail `json:"details,omitempty"`
	InnerError *InnerError   `json:"innerError,omitempty"`
}

type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Target  string `json:"target,omitempty"`
}

// InnerError is microsoft.graph.ODataErrors.InnerError; two names carry hyphens.
type InnerError struct {
	RequestID       string `json:"request-id,omitempty"`
	ClientRequestID string `json:"client-request-id,omitempty"`
	Date            string `json:"date,omitempty" jsonschema:"format=date-time,description=Date when the error occured."`
}

// User is the common subset of microsoft.graph.user, which inherits directoryObject.
type User struct {
	ID                string `json:"id,omitempty" jsonschema:"description=The unique identifier for the user."`
	DisplayName       string `json:"displayName,omitempty" jsonschema:"description=The name displayed in the address book for the user."`
	Mail              string `json:"mail,omitempty" jsonschema:"description=The SMTP address for the user."`
	UserPrincipalName string `json:"userPrincipalName,omitempty" jsonschema:"description=The user principal name (UPN) of the user."`
	GivenName         string `json:"givenName,omitempty" jsonschema:"description=The given name (first name) of the user."`
	Surname           string `json:"surname,omitempty" jsonschema:"description=The user's surname (family name or last name)."`
	JobTitle          string `json:"jobTitle,omitempty" jsonschema:"description=The user's job title."`
	AccountEnabled    bool   `json:"accountEnabled,omitempty" jsonschema:"description=true if the account is enabled; otherwise, false."`
	CreatedDateTime   string `json:"createdDateTime,omitempty" jsonschema:"format=date-time,description=The date and time the user was created."`
}

// UserCollectionResponse is microsoft.graph.userCollectionResponse: the page
// links from BaseCollectionPaginationCountResponse, then value.
type UserCollectionResponse struct {
	ODataCount    int64  `json:"@odata.count,omitempty"`
	ODataNextLink string `json:"@odata.nextLink,omitempty"`
	Value         []User `json:"value"`
}

type Message struct {
	Subject      string      `json:"subject,omitempty"`
	Body         ItemBody    `json:"body,omitempty"`
	ToRecipients []Recipient `json:"toRecipients,omitempty"`
}

type ItemBody struct {
	Content     string `json:"content,omitempty" jsonschema:"description=The content of the item."`
	ContentType string `json:"contentType,omitempty" jsonschema:"description=The type of the content. Possible values are text and html.,enum=text|html"`
}

type Recipient struct {
	EmailAddress EmailAddress `json:"emailAddress,omitempty"`
}

type EmailAddress struct {
	Name    string `json:"name,omitempty" jsonschema:"description=The display name of the person or entity."`
	Address string `json:"address,omitempty" jsonschema:"description=The email address of the person or entity."`
}

type (
	// getMeReq is me.user.GetUser: ConsistencyLevel plus $select/$expand.
	getMeReq struct {
		ConsistencyLevel string   `header:"ConsistencyLevel,omitempty" jsonschema:"description=Indicates the requested consistency level."`
		Select           []string `query:"$select,omitempty" jsonschema:"description=Select properties to be returned"`
		Expand           []string `query:"$expand,omitempty" jsonschema:"description=Expand related entities"`
	}
	// listUsersReq is users.user.ListUser, in published order.
	listUsersReq struct {
		ConsistencyLevel string   `header:"ConsistencyLevel,omitempty" jsonschema:"description=Indicates the requested consistency level."`
		Top              int32    `query:"$top,omitempty" jsonschema:"description=Show only the first n items,minimum=0,example=50"`
		Search           string   `query:"$search,omitempty" jsonschema:"description=Search items by search phrases"`
		Filter           string   `query:"$filter,omitempty" jsonschema:"description=Filter items by property values"`
		Count            bool     `query:"$count,omitempty" jsonschema:"description=Include count of items"`
		OrderBy          []string `query:"$orderby,omitempty" jsonschema:"description=Order items by property values"`
		Select           []string `query:"$select,omitempty" jsonschema:"description=Select properties to be returned"`
		Expand           []string `query:"$expand,omitempty" jsonschema:"description=Expand related entities"`
	}
	// getUserReq is users.user.GetUser: no body, so user-id rides the Req type.
	getUserReq struct {
		UserID string   `path:"user-id" jsonschema:"description=The unique identifier of user"`
		Select []string `query:"$select,omitempty" jsonschema:"description=Select properties to be returned"`
		Expand []string `query:"$expand,omitempty" jsonschema:"description=Expand related entities"`
	}
	deleteUserReq struct {
		UserID  string `path:"user-id" jsonschema:"description=The unique identifier of user"`
		IfMatch string `header:"If-Match,omitempty" jsonschema:"description=ETag"`
	}
	// mediaReq is the profile-photo media operation: path parameter only.
	mediaReq struct {
		UserID string `path:"user-id" jsonschema:"description=The unique identifier of user"`
	}
	// sendMailReq is sendMail: the path parameter plus the body properties,
	// which specout splits out into their own component.
	sendMailReq struct {
		UserID          string  `path:"user-id" jsonschema:"description=The unique identifier of user"`
		Message         Message `json:"Message,omitempty" jsonschema:"description=The message to send."`
		SaveToSentItems bool    `json:"SaveToSentItems,omitempty" jsonschema:"description=Save the message in Sent Items."`
	}
)
