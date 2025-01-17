package web_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/smartcontractkit/chainlink/core/services/eth"

	"github.com/pelletier/go-toml"

	"github.com/smartcontractkit/chainlink/core/internal/cltest"
	"github.com/smartcontractkit/chainlink/core/services/offchainreporting"
	"github.com/smartcontractkit/chainlink/core/services/pipeline"
	"github.com/smartcontractkit/chainlink/core/store/models"
	"github.com/smartcontractkit/chainlink/core/web"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/guregu/null.v4"
)

func TestPipelineRunsController_Create_HappyPath(t *testing.T) {
	t.Parallel()
	rpcClient, gethClient, _, assertMocksCalled := cltest.NewEthMocksWithStartupAssertions(t)
	defer assertMocksCalled()
	app, cleanup := cltest.NewApplication(t,
		eth.NewClientWith(rpcClient, gethClient),
	)
	defer cleanup()
	require.NoError(t, app.Start())
	key := cltest.MustInsertRandomKey(t, app.Store.DB)

	client := app.NewHTTPClient()

	var ocrJobSpecFromFile offchainreporting.OracleSpec
	tree, err := toml.LoadFile("testdata/oracle-spec.toml")
	require.NoError(t, err)
	err = tree.Unmarshal(&ocrJobSpecFromFile)
	require.NoError(t, err)

	ocrJobSpecFromFile.TransmitterAddress = &key.Address

	jobID, _ := app.AddJobV2(context.Background(), ocrJobSpecFromFile, null.String{})

	response, cleanup := client.Post("/v2/jobs/"+fmt.Sprintf("%v", jobID)+"/runs", nil)
	defer cleanup()
	cltest.AssertServerResponse(t, response, http.StatusOK)

	parsedResponse := models.PipelineRun{}
	err = web.ParseJSONAPIResponse(cltest.ParseResponseBody(t, response), &parsedResponse)
	assert.NoError(t, err)
	assert.NotNil(t, parsedResponse.ID)
}

func TestPipelineRunsController_Index_HappyPath(t *testing.T) {
	client, jobID, runIDs, cleanup := setupPipelineRunsControllerTests(t)
	defer cleanup()

	response, cleanup := client.Get("/v2/jobs/" + fmt.Sprintf("%v", jobID) + "/runs")
	defer cleanup()
	cltest.AssertServerResponse(t, response, http.StatusOK)

	var parsedResponse []pipeline.Run
	responseBytes := cltest.ParseResponseBody(t, response)
	assert.Contains(t, string(responseBytes), `"meta":null,"errors":[null],"outputs":["3"]`)

	err := web.ParseJSONAPIResponse(responseBytes, &parsedResponse)
	assert.NoError(t, err)

	require.Len(t, parsedResponse, 2)
	assert.Equal(t, parsedResponse[1].ID, runIDs[0])
	assert.NotNil(t, parsedResponse[1].CreatedAt)
	assert.NotNil(t, parsedResponse[1].FinishedAt)
	require.Len(t, parsedResponse[1].PipelineTaskRuns, 4)
}

func TestPipelineRunsController_Index_Pagination(t *testing.T) {
	client, jobID, runIDs, cleanup := setupPipelineRunsControllerTests(t)
	defer cleanup()

	response, cleanup := client.Get("/v2/jobs/" + fmt.Sprintf("%v", jobID) + "/runs?page=1&size=1")
	defer cleanup()
	cltest.AssertServerResponse(t, response, http.StatusOK)

	var parsedResponse []pipeline.Run
	responseBytes := cltest.ParseResponseBody(t, response)
	assert.Contains(t, string(responseBytes), `"meta":null,"errors":[null],"outputs":["3"]`)
	assert.Contains(t, string(responseBytes), `"meta":{"count":2}`)

	err := web.ParseJSONAPIResponse(responseBytes, &parsedResponse)
	assert.NoError(t, err)

	require.Len(t, parsedResponse, 1)
	assert.Equal(t, parsedResponse[0].ID, runIDs[1])
	assert.NotNil(t, parsedResponse[0].CreatedAt)
	assert.NotNil(t, parsedResponse[0].FinishedAt)
	require.Len(t, parsedResponse[0].PipelineTaskRuns, 4)
}

func TestPipelineRunsController_Show_HappyPath(t *testing.T) {
	client, jobID, runIDs, cleanup := setupPipelineRunsControllerTests(t)
	defer cleanup()

	response, cleanup := client.Get("/v2/jobs/" + fmt.Sprintf("%v", jobID) + "/runs/" + fmt.Sprintf("%v", runIDs[0]))
	defer cleanup()
	cltest.AssertServerResponse(t, response, http.StatusOK)

	var parsedResponse pipeline.Run
	responseBytes := cltest.ParseResponseBody(t, response)
	assert.Contains(t, string(responseBytes), `"meta":null,"errors":[null],"outputs":["3"]`)

	err := web.ParseJSONAPIResponse(responseBytes, &parsedResponse)
	assert.NoError(t, err)

	assert.Equal(t, parsedResponse.ID, runIDs[0])
	assert.NotNil(t, parsedResponse.CreatedAt)
	assert.NotNil(t, parsedResponse.FinishedAt)
	require.Len(t, parsedResponse.PipelineTaskRuns, 4)
}

func TestPipelineRunsController_ShowRun_InvalidID(t *testing.T) {
	t.Parallel()
	rpcClient, gethClient, _, assertMocksCalled := cltest.NewEthMocksWithStartupAssertions(t)
	defer assertMocksCalled()
	app, cleanup := cltest.NewApplication(t,
		eth.NewClientWith(rpcClient, gethClient),
	)
	defer cleanup()
	require.NoError(t, app.Start())
	client := app.NewHTTPClient()

	response, cleanup := client.Get("/v2/jobs/1/runs/invalid-run-ID")
	defer cleanup()
	cltest.AssertServerResponse(t, response, http.StatusUnprocessableEntity)
}

func setupPipelineRunsControllerTests(t *testing.T) (cltest.HTTPClientCleaner, int32, []int64, func()) {
	t.Parallel()
	rpcClient, gethClient, _, assertMocksCalled := cltest.NewEthMocksWithStartupAssertions(t)
	defer assertMocksCalled()
	app, cleanup := cltest.NewApplication(t,
		eth.NewClientWith(rpcClient, gethClient),
	)
	require.NoError(t, app.Start())
	client := app.NewHTTPClient()
	mockHTTP, cleanupHTTP := cltest.NewHTTPMockServer(t, http.StatusOK, "GET", `{"USD": 1}`)

	key := cltest.MustInsertRandomKey(t, app.Store.DB)

	var ocrJobSpec offchainreporting.OracleSpec
	err := toml.Unmarshal([]byte(fmt.Sprintf(`
	type               = "offchainreporting"
	schemaVersion      = 1
	contractAddress    = "%s"
	p2pPeerID          = "%s"
	p2pBootstrapPeers  = [
		"/dns4/chain.link/tcp/1234/p2p/16Uiu2HAm58SP7UL8zsnpeuwHfytLocaqgnyaYKP8wu7qRdrixLju",
	]
	keyBundleID        = "%s"
	transmitterAddress = "%s"
	observationSource = """
		// data source 1
		ds          [type=http method=GET url="%s"];
		ds_parse    [type=jsonparse path="USD"];
		ds_multiply [type=multiply times=3];

		ds -> ds_parse -> ds_multiply -> answer;

		answer [type=median index=0];
	"""
	`, cltest.NewAddress().Hex(), cltest.DefaultP2PPeerID, cltest.DefaultOCRKeyBundleID, key.Address.Hex(), mockHTTP.URL)), &ocrJobSpec)
	require.NoError(t, err)

	jobID, err := app.AddJobV2(context.Background(), ocrJobSpec, null.String{})
	require.NoError(t, err)

	firstRunID, err := app.RunJobV2(context.Background(), jobID, nil)
	require.NoError(t, err)
	secondRunID, err := app.RunJobV2(context.Background(), jobID, nil)
	require.NoError(t, err)

	err = app.AwaitRun(context.Background(), firstRunID)
	require.NoError(t, err)
	err = app.AwaitRun(context.Background(), secondRunID)
	require.NoError(t, err)

	return client, jobID, []int64{firstRunID, secondRunID}, func() {
		cleanup()
		cleanupHTTP()
	}
}
