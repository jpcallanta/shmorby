// Package tui implements the Bubbletea-based terminal UI.
package tui

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"shmorby/internal/agent"
	ctxcomp "shmorby/internal/context"
	"shmorby/internal/llm"
	"shmorby/internal/memory"
	"shmorby/internal/session"
	"shmorby/internal/tools"
	tuicl "shmorby/internal/tui/clipboard"
	tuicompl "shmorby/internal/tui/completion"
	"shmorby/internal/tui/history"
	"shmorby/internal/tui/keybinds"
	"shmorby/internal/tui/navigation"
	"shmorby/internal/tui/palette"
	tuirender "shmorby/internal/tui/render"
	"shmorby/internal/tui/sessiontab"
	"shmorby/internal/tui/spinner"
	"shmorby/internal/tui/styles"
	tuivp "shmorby/internal/tui/viewport"
)

// Messages sent through the Bubbletea update loop.
type submitMsg struct{ text string }
type agentReplyMsg struct {
	text          string
	memoryEntries int
}
type toolStatusMsg struct {
	name   string
	status string
}
type errorMsg struct{ err error }
type outputMsg struct{ text string }
type spinnerTickMsg struct{}

// Streaming messages.
type streamDeltaMsg struct {
	delta string
}
type streamDoneMsg struct{}

// Permission messages.
type permissionResultMsg struct {
	choice PermissionChoice
}
type permissionReqMsg struct {
	prompt PermissionPrompt
}

// settleMsg signals that the stream settle timer has expired.
type settleMsg struct{}

// spinnerStopMsg stops the spinner after output is rendered.
type spinnerStopMsg struct{}

// agentModeChangedMsg signals that the agent mode was switched.
type agentModeChangedMsg struct {
	mode string
}

// leaderTimeoutMsg signals that the leader key sequence timed out.
type leaderTimeoutMsg struct{}

// Logging messages.
type logEntryMsg struct {
	entry LogEntry
}
type thinkingDeltaMsg struct {
	delta string
}
type thinkingEndMsg struct{}
type setLogLevelMsg struct {
	level slog.Level
}
type agentEventMsg struct {
	event agent.AgentEvent
}

// outputEntry is a single line in the scrollable output pane.
type outputEntry struct {
	kind string // "user", "agent", "tool", "error"
	text string
}

// Model is the top-level Bubbletea model.
type Model struct {
	// Input
	textarea textarea.Model

	// Output
	output   []outputEntry
	viewport tuivp.Model

	// Streaming
	streamBuf StreamBuffer

	// Spinner
	spinner     spinner.Model
	spinnerText string
	startTime   time.Time
	tokensDown  int

	// State
	running    bool
	fullscreen bool
	width      int
	height     int

	// Agent
	provider  llm.Provider
	session   *session.Session
	mode      string
	scope     string
	model     string
	override  string
	registry  *tools.Registry
	maxIter   int
	shell     bool
	scopeInfo ScopeInfo

	// Theme
	theme styles.Theme

	// Context cancel
	cancel context.CancelFunc

	// Slash-command completion
	complEngine    *tuicompl.Engine
	complMatches   []tuicompl.Command
	complIdx       int
	showCompletion bool

	// Permission prompt
	permission *PermissionPrompt

	// Current tool being executed
	currentTool       string
	currentToolStatus string

	// Output selection
	selectionMode  bool
	selectionStart int
	selectionEnd   int
	copyNotify     string
	copyNotifyTime time.Time

	// Configuration
	glamourEnabled bool
	scrollLines    int

	// Stream settle timer
	settleTimer *time.Timer

	// Memory
	memoryStore memory.Store
	retriever   *memory.Retriever

	// Context compression
	compressor *ctxcomp.Compressor
	modelInfo  llm.ModelInfo
	ctxStats   *CtxStats

	// Pending actions for confirmation prompts
	pendingClearMemory bool
	haltPrompt         bool

	// Phase 19: navigation components
	modeSwitcher      *navigation.ModeSwitcher
	referenceEngine   *navigation.ReferenceEngine
	shellCmdHandler   *navigation.ShellCmdHandler
	scrollAccel       *navigation.ScrollAcceleration
	leaderKey         *keybinds.LeaderKey
	whichKey          *keybinds.WhichKeyModel
	commandPalette    *palette.CommandPalette
	inputHistory      *history.History
	reverseSearch     *history.ReverseISearch
	tabBar            *sessiontab.TabBar
	showReverseSearch bool

	// Phase 20: logging
	logEntries           []LogEntry
	logExpanded          bool
	logLevel             slog.Level
	thinking             ThinkingBuffer
	thinkingExpanded     bool
	logChan              chan LogEntry
	logMaxEntries        int
	logDisplayLimit      int
	logCollapse          bool
	logCollapseThreshold int
	logHandler           *TUILogHandler

	// Agent event channel (tool status from agent loop).
	agentEventChan chan agent.AgentEvent

	// Phase 21: help overlay
	showHelp *HelpModel

	// Phase 26: interactive permission prompts
	permissionReqChan chan PermissionPrompt
	toolRules         map[string]*tools.RuleSet
}

// CtxStats holds compression and token usage statistics for display.
type CtxStats struct {
	EstimatedTokens   int
	ContextWindow     int
	Compressions      int
	Mode              string
	Fallback          bool
	OffloadedMessages int
	StorageUsedBytes  int64
}

// ScopeInfo holds scope metadata for the /scope command.
type ScopeInfo struct {
	PrimaryPath  string
	Instructions []string
	TotalBytes   int
}

// Fun messages shown in the spinner during LLM thinking.
var thinkingMessages = []string{
	"thinking…",
	"pondering…",
	"deep in thought…",
	"connecting the dots…",
	"sharpening the razor…",
	"reticulating splines…",
	"gathering thoughts…",
	"feeding the neurons…",
	"counting stars…",
	"petting the cat…",
	"stirring the pot…",
	"baking cookies…",
	"sharpening pencils…",
	"rolling the dice…",
	"chasing rabbits…",
	"climbing the tree…",
	"following the breadcrumbs…",
	"exploring the unknown…",
	"charting the stars…",
	"mapping the terrain…",
	"decoding the matrix…",
	"unlocking secrets…",
	"weaving the web…",
	"spinning the thread…",
	"polishing the gem…",
	"forging the sword…",
	"brewing the potion…",
	"lighting the fuse…",
	"winding the clock…",
	"tuning the instrument…",
	"painting the canvas…",
	"sculpting the clay…",
	"writing the symphony…",
	"editing the manuscript…",
	"composing the verse…",
	"chiseling the marble…",
	"baking the bread…",
	"fermenting the wine…",
	"aging the cheese…",
	"tending the garden…",
	"watering the plants…",
	"pruning the bonsai…",
	"folding the paper…",
	"tying the knot…",
	"stitching the quilt…",
	"mending the net…",
	"casting the line…",
	"setting the trap…",
	"baiting the hook…",
	"diving the reef…",
	"surfing the waves…",
	"riding the wind…",
	"soaring the skies…",
	"digging the tunnel…",
	"building the bridge…",
	"laying the foundation…",
	"raising the roof…",
	"hanging the door…",
	"glazing the window…",
	"wiring the circuit…",
	"soldering the joint…",
	"welding the seam…",
	"riveting the plate…",
	"bolting the frame…",
	"oiling the gears…",
	"greasing the wheels…",
	"firing the engine…",
	"launching the rocket…",
	"plotting the course…",
	"navigating the fog…",
	"charting the depths…",
	"sounding the horn…",
	"hoisting the sail…",
	"trimming the mast…",
	"battening the hatches…",
	"scanning the horizon…",
	"calculating the odds…",
	"measuring twice…",
	"cutting once…",
	"weighing the options…",
	"balancing the scales…",
	"sorting the pieces…",
	"arranging the deck…",
	"shuffling the pack…",
	"dealing the cards…",
	"playing the hand…",
	"raising the stakes…",
	"calling the bluff…",
	"checking the mate…",
	"moving the pawn…",
	"castling the king…",
	"opening the book…",
	"reading the signs…",
	"interpreting the results…",
	"analyzing the data…",
	"crunching the numbers…",
	"running the simulation…",
	"training the model…",
	"optimizing the parameters…",
	"tuning the hyperparameters…",
	"minimizing the loss…",
	"maximizing the reward…",
	"exploring the state space…",
	"exploiting the policy…",
	"backpropagating the error…",
	"updating the weights…",
	"normalizing the inputs…",
	"regularizing the output…",
	"augmenting the dataset…",
	"balancing the classes…",
	"cross-validating the folds…",
	"bootstrapping the samples…",
	"bagging the predictors…",
	"boosting the ensemble…",
	"stacking the models…",
	"blending the predictions…",
	"extracting the features…",
	"reducing the dimensions…",
	"clustering the points…",
	"classifying the instances…",
	"regressing the targets…",
	"transforming the distribution…",
	"fitting the curve…",
	"interpolating the gaps…",
	"extrapolating the trends…",
	"smoothing the noise…",
	"filtering the outliers…",
	"imputing the missing…",
	"encoding the categories…",
	"tokenizing the text…",
	"embedding the words…",
	"attending to the context…",
	"encoding the sequence…",
	"decoding the output…",
	"translating the language…",
	"summarizing the document…",
	"paraphrasing the paragraph…",
	"generating the response…",
	"reasoning about the problem…",
	"planning the steps…",
	"solving the subproblem…",
	"decomposing the task…",
	"composing the solution…",
	"verifying the result…",
	"validating the hypothesis…",
	"testing the assumption…",
	"debugging the code…",
	"refactoring the module…",
	"reviewing the diff…",
	"merging the branches…",
	"resolving the conflict…",
	"rebasing the commits…",
	"squashing the history…",
	"cherry-picking the fix…",
	"bisecting the regression…",
	"profiling the bottleneck…",
	"optimizing the hot path…",
	"caching the response…",
	"indexing the database…",
	"querying the store…",
	"aggregating the results…",
	"streaming the events…",
	"buffering the packets…",
	"serializing the objects…",
	"deserializing the payload…",
	"compressing the data…",
	"encrypting the traffic…",
	"hashing the password…",
	"salting the hash…",
	"signing the certificate…",
	"verifying the chain…",
	"parsing the grammar…",
	"lexing the input…",
	"compiling the source…",
	"linking the libraries…",
	"packaging the binary…",
	"deploying the service…",
	"orchestrating the containers…",
	"scheduling the jobs…",
	"provisioning the resources…",
	"scaling the cluster…",
	"balancing the load…",
	"routing the traffic…",
	"routing the request…",
	"serving the content…",
	"caching the page…",
	"rendering the template…",
	"hydrating the component…",
	"mounting the view…",
	"dispatching the action…",
	"reducing the state…",
	"subscribing to the store…",
	"emitting the event…",
	"listening to the channel…",
	"acknowledging the receipt…",
	"retrying the attempt…",
	"falling back to safe mode…",
	"degrading gracefully…",
	"recovering from failure…",
	"replicating the data…",
	"sharding the table…",
	"partitioning the dataset…",
	"distributing the workload…",
	"coordinating the nodes…",
	"consensus on the value…",
	"committing the transaction…",
	"rolling back the change…",
	"logging the entry…",
	"tracing the request…",
	"monitoring the metric…",
	"alerting on the anomaly…",
	"visualizing the trend…",
	"reporting the status…",
	"auditing the trail…",
	"complying with the policy…",
	"governing the access…",
	"authenticating the user…",
	"authorizing the action…",
	"auditing the event…",
	"safeguarding the secret…",
	"rotating the key…",
	"revoking the token…",
	"refreshing the session…",
	"renewing the lease…",
	"cleaning the cache…",
	"vacuuming the table…",
	"defragmenting the index…",
	"reindexing the corpus…",
	"rebuilding the index…",
	"purging the old data…",
	"archiving the records…",
	"backing up the state…",
	"restoring the snapshot…",
	"checkpointing the progress…",
	"snapshotting the volume…",
	"cloning the repository…",
	"fetching the updates…",
	"pulling the changes…",
	"pushing the commits…",
	"tagging the release…",
	"branching the feature…",
	"patching the vulnerability…",
	"upgrading the dependency…",
	"vendorizing the library…",
	"pinning the version…",
	"resolving the dependency…",
	"pruning the tree…",
	"auditing the supply chain…",
	"scanning the image…",
	"linting the code…",
	"formatting the style…",
	"type-checking the signatures…",
	"testing the unit…",
	"integrating the modules…",
	"end-to-end testing the flow…",
	"stress-testing the limits…",
	"benchmarking the performance…",
	"profiling the memory…",
	"debugging the goroutine…",
	"tracing the syscall…",
	"inspecting the heap…",
	"dumping the stack…",
	"disassembling the binary…",
	"reverse-engineering the protocol…",
	"fuzzing the input…",
	"mocking the service…",
	"stubbing the endpoint…",
	"fixturing the data…",
	"seeding the database…",
	"migrating the schema…",
	"bootstrapping the app…",
	"wiring the dependencies…",
	"injecting the config…",
	"resolving the context…",
	"spinning up the server…",
	"listening on the port…",
	"accepting connections…",
	"handling the request…",
	"middlewaring the pipeline…",
	"intercepting the call…",
	"decorating the response…",
	"wrapping the error…",
	"recovering the panic…",
	"shutting down gracefully…",
	"waiting for goroutines…",
	"draining the connections…",
	"closing the file handles…",
	"releasing the memory…",
	"unmounting the filesystem…",
	"unbinding the socket…",
	"stopping the timer…",
	"cancelling the context…",
	"cleaning up the temp…",
	"tidying the modules…",
	"syncing the filesystem…",
	"flushing the buffers…",
	"finalizing the transaction…",
	"persisting the state…",
	"freezing the frame…",
	"capturing the moment…",
	"framing the picture…",
	"developing the film…",
	"printing the photo…",
	"binding the book…",
	"marking the page…",
	"folding the corner…",
	"breaking the spine…",
	"dog-earing the page…",
	"highlighting the text…",
	"annotating the margin…",
	"bookmarking the spot…",
	"tabbing the reference…",
	"citing the source…",
	"quoting the expert…",
	"referencing the paper…",
	"bibliographing the works…",
	"indexing the terms…",
	"glossarying the definitions…",
	"appending the notes…",
	"prepending the summary…",
	"inserting the chapter…",
	"deleting the digression…",
	"revising the draft…",
	"proofreading the copy…",
	"copyediting the prose…",
	"fact-checking the claims…",
	"peer-reviewing the submission…",
	"accepting the manuscript…",
	"publishing the edition…",
	"distributing the copies…",
	"promoting the work…",
	"collecting the royalties…",
	"dusting the shelves…",
	"organizing the files…",
	"labeling the folders…",
	"categorizing the items…",
	"tagging the resources…",
	"curating the collection…",
	"preserving the artifacts…",
	"restoring the painting…",
	"conserving the energy…",
	"harvesting the solar…",
	"storing the battery…",
	"inverting the current…",
	"rectifying the signal…",
	"amplifying the gain…",
	"attenuating the noise…",
	"modulating the frequency…",
	"demodulating the carrier…",
	"encoding the message…",
	"transmitting the packet…",
	"receiving the acknowledge…",
	"echoing the ping…",
	"tracerting the hops…",
	"resolving the hostname…",
	"looking up the address…",
	"connecting to the peer…",
	"shaking hands with the server…",
	"negotiating the TLS…",
	"exchanging the keys…",
	"establishing the tunnel…",
	"forwarding the port…",
	"redirecting the flow…",
	"load-balancing the requests…",
	"failover to the replica…",
	"restoring from backup…",
	"following the leader…",
	"electing the coordinator…",
	"joining the cluster…",
	"leaving the group…",
	"broadcasting the message…",
	"multicasting the update…",
	"unicasting the reply…",
	"polling the queue…",
	"publishing the topic…",
	"subscribing to the feed…",
	"consuming the event…",
	"producing the record…",
	"partitioning the stream…",
	"offsetting the cursor…",
	"committing the offset…",
	"rewinding the log…",
	"replaying the history…",
	"snapshotting the state…",
	"compacting the topic…",
	"mirroring the cluster…",
	"federating the query…",
	"aggregating the metrics…",
	"downsampling the series…",
	"rolling up the data…",
	"granularizing the intervals…",
	"forecasting the demand…",
	"detecting the seasonality…",
	"removing the trend…",
	"decomposing the series…",
	"auto-correlating the lags…",
	"cross-correlating the signals…",
	"convolving the kernels…",
	"pooling the features…",
	"flattening the tensor…",
	"reshaping the matrix…",
	"transposing the axes…",
	"broadcasting the dimensions…",
	"dotting the product…",
	"crossing the vectors…",
	"normalizing the batch…",
	"dropouting the neurons…",
	"activating the relu…",
	"softmaxing the logits…",
	"tanh-ing the hidden…",
	"sigmoiding the output…",
	"embedding the lookup…",
	"positional-encoding the sequence…",
	"masking the attention…",
	"multi-head-attending…",
	"layer-normalizing the activations…",
	"residual-connecting the blocks…",
	"feed-forwarding the network…",
	"gating the recurrency…",
	"LSTM-ing the memory…",
	"GRU-ing the cell…",
	"transforming the encoder…",
	"decoding the autoregressive…",
	"beam-searching the paths…",
	"top-k sampling the tokens…",
	"top-p sampling the nucleus…",
	"temperature-scaling the logits…",
	"repetition-penalizing the ngrams…",
	"length-penalizing the sequences…",
	"early-stopping the generation…",
	"cueing the prompt…",
	"instruction-following the task…",
	"role-playing the assistant…",
	"system-prompty the behavior…",
	"few-shotting the examples…",
	"zero-shotting the inference…",
	"chain-of-thought reasoning…",
	"tree-of-thought exploring…",
	"reflecting on the answer…",
	"critiquing the output…",
	"refining the response…",
	"formatting the markdown…",
	"syntax-highlighting the code…",
	"rendering the table…",
	"embedding the image…",
	"linking the reference…",
	"footnoting the citation…",
	"tweaking the style…",
	"honing the edge…",
	"buffing the surface…",
	"varnishing the finish…",
	"sealing the deal…",
	"crossing the t…",
	"dotting the i…",
	"trimming the fat…",
	"cutting the corners…",
	"sanding the rough edges…",
	"filling the cracks…",
	"priming the canvas…",
	"sketching the outline…",
	"blocking the shapes…",
	"layering the color…",
	"mixing the palette…",
	"glazing the tone…",
	"highlighting the value…",
	"shadowing the form…",
	"texturing the surface…",
	"stippling the dots…",
	"hatching the lines…",
	"cross-hatching the depth…",
	"blending the strokes…",
	"erasing the mistakes…",
	"starting over…",
	"taking a break…",
	"stretching the legs…",
	"refilling the coffee…",
	"resting the eyes…",
	"clearing the mind…",
	"finding the zone…",
	"entering the flow…",
}

// Fun messages shown in the spinner while a tool is running.
var waitingMessages = []string{
	"waiting for tool",
	"wrangling the daemons…",
	"consulting the oracle…",
	"summoning the ancients…",
	"polishing the pipelines…",
	"feeding the gremlins…",
	"herding the processes…",
	"tickling the kernel…",
	"warming up the cache…",
	"negotiating with the kernel…",
	"untangling the pipes…",
	"chasing SIGCHLD…",
	"awaiting the oracle…",
	"calming the daemons…",
	"sieving the bits…",
	"untarring the archives…",
	"waiting for the stars to align…",
	"waiting for the tide to turn…",
	"waiting for the paint to dry…",
	"waiting for the dust to settle…",
	"waiting for the fog to lift…",
	"waiting for the penny to drop…",
	"waiting for the other shoe…",
	"waiting for Godot…",
	"holding the line…",
	"holding the fort…",
	"holding the phone…",
	"holding the thought…",
	"keeping the seat warm…",
	"keeping the home fires burning…",
	"keeping the candle lit…",
	"watching the clock…",
	"watching the pot…",
	"watching the grass grow…",
	"watching the paint dry…",
	"counting the seconds…",
	"counting the minutes…",
	"counting the hours…",
	"marking the time…",
	"killing the time…",
	"passing the buck…",
	"passing the time…",
	"spinning the wheels…",
	"biding the time…",
	"stalling the engine…",
	"idling the car…",
	"drifting the current…",
	"floating the boat…",
	"anchoring the ship…",
	"dropping the anchor…",
	"reefing the sail…",
	"heaving the line…",
	"securing the load…",
	"battening down…",
	"holing up…",
	"hunkering down…",
	"digging in…",
	"standing guard…",
	"standing watch…",
	"keeping vigil…",
	"keeping the faith…",
	"keeping the peace…",
	"keeping quiet…",
	"keeping still…",
	"freezing the frame…",
	"pausing the tape…",
	"buffering the stream…",
	"loading the asset…",
	"preloading the chunk…",
	"fetching the resource…",
	"downloading the artifact…",
	"uploading the result…",
	"syncing the data…",
	"backing up the file…",
	"copying the bytes…",
	"moving the bits…",
	"queuing the job…",
	"scheduling the task…",
	"enqueuing the request…",
	"dequeuing the work…",
	"processing the batch…",
	"handling the item…",
	"dispatching the worker…",
	"pooling the connections…",
	"throttling the rate…",
	"backpressuring the stream…",
	"debouncing the input…",
	"throttling the output…",
	"batching the records…",
	"chunking the data…",
	"streaming the bytes…",
	"piping the output…",
	"tee-ing the stream…",
	"multiplexing the channel…",
	"demultiplexing the signal…",
	"interleaving the frames…",
	"deinterlacing the video…",
	"transcoding the format…",
	"transmuxing the container…",
	"remuxing the streams…",
	"segmenting the file…",
	"chunking the playlist…",
	"keyframing the video…",
	"thumbnail-generating the scene…",
	"watermarking the frame…",
	"subtitle-burning the text…",
	"caption-embedding the metadata…",
	"normalizing the loudness…",
	"equalizing the frequencies…",
	"compressing the dynamic range…",
	"limiting the peak…",
	"gateing the noise…",
	"reverbing the ambiance…",
	"delaying the echo…",
	"phasing the signal…",
	"flanging the chorus…",
	"chorusing the voices…",
	"vocoding the speech…",
	"autotuning the pitch…",
	"quantizing the timing…",
	"groove-swining the beat…",
	"sidechaining the compression…",
	"parallel-compressing the bus…",
	"bus-sending the track…",
	"aux-returning the effect…",
	"panning the stereo…",
	"balancing the levels…",
	"fading the intro…",
	"crossfading the transition…",
	"automating the parameter…",
	"recording the take…",
	"overdubbing the layer…",
	"punching the region…",
	"comping the tracks…",
	"mixing the stems…",
	"mastering the final…",
	"dithering the bit-depth…",
	"sample-rate-converting…",
	"time-stretching the tempo…",
	"pitch-shifting the harmony…",
	"beat-mapping the grid…",
	"warping the transient…",
	"stretching the loop…",
	"rendering the mix…",
	"exporting the wav…",
	"encoding the mp3…",
	"tagging the metadata…",
	"cue-sheeting the tracklist…",
	"printing the stem…",
	"zeroing the levels…",
	"patching the cable…",
	"routing the signal…",
	"gain-staging the chain…",
	"phantom-powering the mic…",
	"DI-boxing the instrument…",
	"reamping the guitar…",
	"mic-ing the cabinet…",
	"positioning the capsule…",
	"polar-patterning the mic…",
	"high-pass-filtering the rumble…",
	"low-pass-filtering the hiss…",
	"notch-filtering the feedback…",
	"parametric-eq-ing the channel…",
	"graphic-eq-ing the room…",
	"room-correcting the monitor…",
	"calibrating the sub…",
	"time-aligning the speakers…",
	"phase-cancelling the bleed…",
	"null-testing the cancellation…",
	"spectrum-analyzing the mix…",
	"loudness-metering the LUFS…",
	"true-peak-metering the intersample…",
	"correlation-metering the stereo…",
	"scope-metering the waveform…",
	"tuning the antenna…",
	"orienting the dish…",
	"locking the satellite…",
	"syncing the timecode…",
	"genlocking the reference…",
	"frame-syncing the cameras…",
	"black-bursting the signal…",
	"color-baring the bars…",
	"waveform-monitoring the video…",
	"vectorscoping the chroma…",
	"histogram-exposing the shot…",
	"false-color-checking the levels…",
	"peaking the focus…",
	"zebra-striping the highlights…",
	"exposure-compensating the aperture…",
	"white-balancing the gel…",
	"color-temp-correcting the kelvin…",
	"LUT-applying the grade…",
	"color-grading the log…",
	"primary-correcting the shot…",
	"secondary-correcting the mask…",
	"power-windowing the face…",
	"tracking the point…",
	"stabilizing the jitter…",
	"motion-blurring the frame…",
	"optical-flow-interpolating…",
	"warp-stabilizing the handheld…",
	"rolling-shutter-correcting…",
	"lens-distortion-correcting…",
	"chromatic-aberration-removing…",
	"vignette-correcting the edge…",
	"denoising the sensor…",
	"sharpening the details…",
	"clarity-punching the midtones…",
	"texture-revealing the grain…",
	"dehazing the atmosphere…",
	"split-toning the shadows…",
	"graduated-filtering the sky…",
	"radial-filtering the subject…",
	"brush-masking the adjustment…",
	"healing the spot…",
	"clone-stamping the blemish…",
	"patch-replacing the artifact…",
	"content-aware-filling the gap…",
	"downscaling the resolution…",
	"upscaling the definition…",
	"super-resolution-inferring…",
	"interlacing the progressive…",
	"deinterlacing the fields…",
	"reverse-telecining the pulldown…",
	"IVTC-ing the film…",
	"match-framing the cut…",
	"edit-deciding the take…",
	"trimming the head…",
	"rippling the edit…",
	"rolling the trim…",
	"slipping the clip…",
	"sliding the segment…",
	"extending the edit…",
	"shortening the gap…",
	"nesting the sequence…",
	"multi-cam-switching the angle…",
	"subclipping the moment…",
	"grouping the tracks…",
	"linking the video…",
	"syncing the audio…",
	"keyframing the motion…",
	"ease-in-the animation…",
	"ease-out-the transition…",
	"bezier-curving the path…",
	"path-following the null…",
	"parenting the layer…",
	"precomposing the elements…",
	"time-remapping the speed…",
	"freeze-framing the moment…",
	"reverse-playing the clip…",
	"speed-ramp-ping the action…",
	"optical-flow-re-timing…",
	"frame-blending the motion…",
	"morph-cutting the transition…",
	"jump-cutting the pause…",
	"match-cutting the action…",
	"L-cutting the audio…",
	"J-cutting the dialogue…",
	"cross-cutting the story…",
	"parallel-editing the scene…",
	"montaging the sequence…",
	"assembling the rough-cut…",
	"fine-cutting the details…",
	"picture-locking the edit…",
	"online-conforming the grade…",
	"offline-editing the proxy…",
	"proxy-attaching the media…",
	"media-managing the project…",
	"archiving the footage…",
	"transcoding the dailies…",
	"ingesting the card…",
	"logging the clips…",
	"bin-organizing the assets…",
	"metadata-tagging the fields…",
	"searching the library…",
	"previewing the clip…",
	"scrubbing the timeline…",
	"jogging the shuttle…",
	"cueing the source…",
	"marking the in-point…",
	"setting the out-point…",
	"clearing the marks…",
	"locating the next edit…",
	"snapping to the grid…",
	"magnet-trimming the edge…",
	"zooming the timeline…",
	"scrolling the sequence…",
	"waveform-drawing the audio…",
	"spectrogram-rendering the sound…",
	"rubber-banding the volume…",
	"pen-tooling the automation…",
	"eyedropper-sampling the color…",
	"ruler-measuring the distance…",
	"grid-aligning the layers…",
	"smart-guide-snapping…",
	"distribution-spacing the objects…",
	"alignment-centering the comp…",
	"sequence-reverse-labeling…",
	"composition-setting the preset…",
	"render-queue-preparing…",
	"output-module-configuring…",
	"watch-folder-scanning…",
	"project-auto-saving…",
	"version-backing-up…",
	"collaboration-locking the comp…",
	"team-project-syncing…",
	"librarian-checking the asset…",
	"render-farm-submitting…",
	"node-wrangling the cores…",
	"distributed-rendering the frame…",
	"bucket-rendering the sequence…",
	"frame-server-streaming…",
	"network-rendering the animation…",
	"GPU-accelerating the effect…",
	"CUDA-processing the matrix…",
	"OpenCL-kernel-compiling…",
	"Metal-shader-precompiling…",
	"Vulkan-pipeline-building…",
	"DirectX-state-caching…",
	"shader-compiling the variant…",
	"texture-compressing the DDS…",
	"mipmap-generating the cascade…",
	"LOD-crossing the distance…",
	"occlusion-culling the mesh…",
	"frustum-culling the camera…",
	"portal-clipping the zone…",
	"BVH-building the acceleration…",
	"octree-splitting the space…",
	"quadtree-subdividing the terrain…",
	"KD-tree-balancing the points…",
	"VP-tree-indexing the metric…",
	"R-tree-sorting the rectangles…",
	"hash-grid-broadphasing the collision…",
	"sweep-and-prune-overlapping…",
	"GJK-detecting the distance…",
	"EPA-expanding the polytope…",
	"impulse-resolving the contact…",
	"constraint-solving the joint…",
	"ragdoll-simulating the physics…",
	"cloth-simulating the draping…",
	"fluid-simulating the particle…",
	"rigid-body-simulating the stack…",
	"soft-body-deforming the mesh…",
	"spring-mass-dampening the system…",
	"Verlet-integrating the position…",
	"Euler-integrating the velocity…",
	"RK4-integrating the accuracy…",
	"Leapfrog-stepping the verlet…",
	"navmesh-generating the walkable…",
	"A-star-pathfinding the route…",
	"Dijkstra-costing the graph…",
	"BFS-flood-filling the region…",
	"DFS-backtracking the maze…",
	"waypoint-smoothing the path…",
	"steering-behaviours-seeking…",
	"flocking-boid-aligning…",
	"formation-keeping the group…",
	"state-machine-transitioning…",
	"behavior-tree-ticking…",
	"utility-AI-scoring the action…",
	"goal-oriented-planning…",
	"hierarchical-task-networking…",
	"FSM-state-transitioning…",
	"decision-tree-pruning…",
	"rule-engine-matching the pattern…",
	"inference-engine-chaining…",
	"expert-system-consulting the KB…",
	"blackboard-architecturing the solution…",
	"subsumption-layering the behavior…",
	"neural-network-learning…",
	"evolutionary-algorithm-generating…",
	"genetic-programming-crossover…",
	"particle-swarm-optimizing…",
	"ant-colony-pheromone-laying…",
	"simulated-annealing-cooling…",
	"tabu-search-memorizing…",
	"hill-climbing-peaking…",
	"greedy-algorithm-choosing…",
	"dynamic-programming-memoizing…",
	"backtracking-recursing…",
	"divide-and-conquer-splitting…",
	"branch-and-bound-pruning…",
	"constraint-satisfaction-propagating…",
	"SAT-solving-clause-learning…",
	"SMT-solving-theory-checking…",
	"CSP-backtracking the domain…",
	"AC-3-arc-consistency-checking…",
	"min-conflict-heuristic-hill-climbing…",
	"walksat-random-flipping…",
	"DPLL-unit-propagating…",
	"CDCL-conflict-driven-learning…",
	"resolution-refutation-proving…",
	"unification-matching the term…",
	"SLD-resolution-deriving…",
	"Prolog-goal-chaining…",
	"Datalog-rule-evaluating…",
	"ASP-answer-set-computing…",
	"description-logic-reasoning…",
	"OWL-ontology-classifying…",
	"RDF-triple-matching…",
	"SPARQL-query-optimizing…",
	"SQL-query-planning…",
	"EXPLAIN-analyzing the plan…",
	"cost-based-optimizer-evaluating…",
	"rule-based-optimizer-transforming…",
	"predicate-pushdown-optimizing…",
	"join-order-enumeration…",
	"nested-loop-joining…",
	"hash-joining the tables…",
	"merge-joining the sorted…",
	"index-only-scanning…",
	"bitmap-index-scanning…",
	"sequential-scanning the table…",
	"parallel-query-processing…",
	"shared-scan-broadcasting…",
	"partition-prune-skipping…",
	"materialized-view-refreshing…",
	"CTE-evaluating the recursive…",
	"window-function-partitioning…",
	"aggregate-hashing the group…",
	"sort-merge-aggregating…",
	"distinct-filtering the duplicates…",
	"limit-pushdown-optimizing…",
	"subquery-flattening the nested…",
	"view-expanding the definition…",
	"trigger-firing the event…",
	"rule-rewriting the query…",
	"constraint-checking the FK…",
	"unique-violation-catching…",
	"deadlock-detecting the cycle…",
	"MVCC-versioning the row…",
	"vacuum-cleaning the dead tuples…",
	"autovacuum-waking the daemon…",
	"checkpoint-flushing the buffer…",
	"WAL-writing the log…",
	"archive-pushing the segment…",
	"recovery-replaying the WAL…",
	"standby-following the primary…",
	"cascading-replication-relaying…",
	"synchronous-commit-acking…",
	"quorum-commit-waiting…",
	"logical-decoding the changes…",
	"change-data-capture-streaming…",
	"schema-migration-locking…",
	"data-type-migrating…",
	"index-creation-building…",
	"reindex-concurrently-building…",
	"cluster-sorting the table…",
	"analyze-statistics-collecting…",
	"explain-analyze-executing…",
	"auto-explain-logging…",
	"pg_stat_statements-querying…",
	"pg_hint_plan-loading…",
	"extension-installing…",
	"foreign-data-wrapper-connecting…",
	"dblink-remote-querying…",
	"postgres_fdw-pushing down…",
	"fdw-scanning the remote…",
	"shard-posting the partition…",
	"citus-distributing the query…",
	"timescale-chunk-compressing…",
	"postGIS-geometry-indexing…",
	"spatial-query-R-tree-scanning…",
	"geography-transform-projecting…",
	"raster-overview-tiling…",
	"point-cloud-LAStools-processing…",
	"h3-hexagon-indexing…",
	"s2-cell-covering…",
	"geohash-prefix-matching…",
	"quadkey-tile-enumerating…",
	"morton-curve-z-ordering…",
	"hilbert-curve-clustering…",
	"space-filling-curve-traversing…",
	"R-tree-bounding-box-checking…",
	"kd-tree-nearest-neighbor-searching…",
	"ball-tree-radius-querying…",
	"LSH-hash-bucketing…",
	"product-quantization-encoding…",
	"IVF-centroid-clustering…",
	"HNSW-graph-traversing…",
	"NSW-inserting the neighbor…",
	"annoy-tree-forest-building…",
	"faiss-index-training…",
	"PQ-code-computing…",
	"OPQ-rotation-learning…",
	"SIMD-distance-computing…",
	"AVX-512-vectorizing…",
	"NEON-intrinsics-optimizing…",
	"CUDA-block-scheduling…",
	"warp-divergence-minimizing…",
	"shared-memory-bank-conflict-avoiding…",
	"global-memory-coalescing…",
	"constant-memory-caching…",
	"texture-memory-binding…",
	"stream-multiprocessor-occupancy…",
	"ILP-instruction-level-parallelizing…",
	"TLP-thread-level-parallelizing…",
	"DLP-data-level-parallelizing…",
	"SIMT-lane-masking…",
	"wavefront-diverge-handling…",
	"tensor-core-matrix-multiplying…",
	"fp16-halving the precision…",
	"int8-quantizing the weight…",
	"int4-quantizing the activation…",
	"sparse-pruning the connection…",
	"distillation-teaching the student…",
	"quantization-aware-training…",
	"pruning-aware-retraining…",
	"knowledge-distillation-transferring…",
	"model-compression-compacting…",
	"ONNX-exporting the graph…",
	"TensorRT-optimizing the engine…",
	"CoreML-converting the model…",
	"TFLite-quantizing the flatbuffer…",
	"OpenVINO-compiling the IR…",
	"MLIR-lowering the dialect…",
	"XLA-compiling the HLO…",
	"TVM-auto-tuning the schedule…",
	"Ansor-designing the template…",
	"AutoScheduler-searching the space…",
	"Halide-scheduling the pipeline…",
	"polyhedral-modeling the loop…",
	"affine-loop-optimizing…",
	"loop-tiling the cache…",
	"loop-unrolling the iteration…",
	"loop-vectorizing the SIMD…",
	"loop-interchanging the nest…",
	"loop-fusion-joining the bodies…",
	"loop-distribution-splitting…",
	"loop-invariant-code-motion-hoisting…",
	"strength-reduction-replacing…",
	"induction-variable-eliminating…",
	"dead-code-eliminating…",
	"common-subexpression-eliminating…",
	"constant-folding-propagating…",
	"copy-propagation-forwarding…",
	"SSA-phi-node-inserting…",
	"dominance-frontier-computing…",
	"control-flow-graph-reducing…",
	"data-flow-analysis-conducting…",
	"liveness-analysis-register-allocating…",
	"register-coloring-graph-coloring…",
	"linear-scan-register-allocating…",
	"greedy-register-allocating…",
	"peephole-optimizing the sequence…",
	"instruction-scheduling…",
	"branch-prediction-hinting…",
	"cache-line-aligning…",
	"prefetch-inserting the instruction…",
	"software-pipelining the loop…",
	"trace-scheduling the basic block…",
	"superblock-forming the hot path…",
	"hyperblock-if-converting…",
	"VLIW-packing the bundle…",
	"EPIC-speculating the predicate…",
	"out-of-order-execution-scheduling…",
	"speculative-execution-guessing…",
	"branch-predictor-training…",
	"return-address-stack-popping…",
	"indirect-branch-target-caching…",
	"TLB-miss-handling…",
	"page-fault-resolving…",
	"swap-write-back-paging…",
	"OOM-killer-sighing…",
	"watchdog-timer-barking…",
	"NMI-interrupt-handling…",
	"IPI-cross-core-signaling…",
	"RCU-grace-period-waiting…",
	"spinlock-ticket-queuing…",
	"mutex-futex-waiting…",
	"rwlock-reader-biasing…",
	"semaphore-count-down…",
	"barrier-synchronizing the thread…",
	"condition-variable-signaling…",
	"completion-variable-waiting…",
	"workqueue-item-queuing…",
	"tasklet-softirq-scheduling…",
	"kthread-waking the daemon…",
	"timer-wheel-expiring…",
	"hrtimer-nanosleep-interrupting…",
	"clockevent-oneshot-programming…",
	"clocksource-read-retrieving…",
	"timekeeper-nsec-updating…",
	"jiffies-counter-rolling…",
	"ktime-get-real-timekeeping…",
	"ntp-sync-adjusting the offset…",
	"PTP-hardware-timestamp-capturing…",
	"chrony-stratum-polling…",
	"ptp4l-delay-request-responding…",
	"phc2sys-clock-synchronizing…",
	"gpsd-pps-pulse-capturing…",
	"beidou-bds-ephemeris-collecting…",
	"galileo-e1-correlating…",
	"glonass-fdma-decoding…",
	"gnss-constellation-triangulating…",
	"imu-accel-gyro-fusing…",
	"ekf-state-estimating…",
	"ukf-unscented-transforming…",
	"particle-filter-resampling…",
	"kalman-filter-predicting…",
	"mahony-filter-quaternion-updating…",
	"madgwick-filter-gradient-descent…",
	"complementary-filter-alpha-blending…",
	"pid-controller-loop-tuning…",
	"LQR-optimal-gain-computing…",
	"MPC-horizon-optimizing…",
	"fft-bin-windowing…",
	"dct-coefficient-quantizing…",
	"wavelet-transform-decomposing…",
	"laplacian-pyramid-blending…",
	"gaussian-pyramid-downsampling…",
	"difference-of-gaussian-detecting blobs…",
	"harris-corner-detecting…",
	"shi-tomasi-good-feature-tracking…",
	"FAST-corner-scoring the pixel…",
	"ORB-descriptor-computing…",
	"BRISK-descriptor-sampling…",
	"FREAK-descriptor-retina-sampling…",
	"AKAZE-descriptor-nonlinear-scaling…",
	"SIFT-descriptor-histogram-binning…",
	"SURF-descriptor-haar-wavelet-summing…",
	"VLAD-vector-of-locally-aggregating…",
	"NetVLAD-layer-convolving…",
	"superpoint-feature-extracting…",
	"superglue-graph-matching…",
	"LoFTR-transformer-attending…",
	"ransac-hypothesis-testing…",
	"prosac-progressive-sampling…",
	"dlt-homography-estimating…",
	"PnP-pose-estimating…",
	"ICP-point-cloud-aligning…",
	"NDT-normal-distributions-transforming…",
	"ESF-ensemble-shape-function-histogramming…",
	"PFH-point-feature-histogram-computing…",
	"FPFH-fast-point-feature-histogram…",
	"VFH-viewpoint-feature-histogram…",
	"GASD-global-aligned-shape-distribution…",
	"CVFH-clustered-viewpoint-feature-histogram…",
	"OUR-CVFH-oriented-reliable…",
	"GRSD-global-radius-based-surface-descriptor…",
	"SHOT-signature-of-histograms-of-orientations…",
	"ROPS-rotational-projection-statistics…",
	"spin-image-2d-accumulating…",
	"3d-shape-context-binning…",
	"mesh-HoG-gradient-computing…",
	"heat-kernel-signature-diffusing…",
	"wave-kernel-signature-oscillating…",
	"GPS-global-point-signature-eigen-decomposing…",
	"mesh-Laplacian-spectral-decomposing…",
	"bilateral-filter-edge-preserving…",
	"taubin-smoothing-the surface…",
	"laplacian-smoothing-the mesh…",
	"poisson-surface-reconstructing…",
	"marching-cubes-isosurface-extracting…",
	"delaunay-triangulation-tetrahedralizing…",
	"voronoi-diagram-bisecting…",
	"alpha-shape-concave-hull-computing…",
	"convex-hull-graham-scan-pointing…",
	"ear-clipping-triangulating the polygon…",
	"sutherland-hodgman-polygon-clipping…",
	"weiler-atherton-polygon-clipping…",
	"greiner-hormann-polygon-clipping…",
	"vatti-polygon-clipping…",
	"scanline-polygon-filling…",
	"flood-fill-seed-pixel-pushing…",
	"boundary-fill-recursive-coloring…",
	"painter-algorithm-depth-sorting…",
	"z-buffer-depth-testing…",
	"z-prepass-early-depth-testing…",
	"hierarchical-z-culling…",
	"ambient-occlusion-SSAO-sampling…",
	"HBAO+hemisphere-sampling…",
	"GTAO-ground-truth-occlusion…",
	"screen-space-reflection-ray-tracing…",
	"ray-tracing-bvh-intersecting…",
	"path-tracing-monte-carlo-sampling…",
	"photon-mapping-caustic-emitting…",
	"metropolis-light-transport-mutating…",
	"bidirectional-path-tracing-connecting…",
	"VCM-vertex-connection-merging…",
	"MLT-mutation-strategy-toggling…",
	"PSSMLT-primary-sample-space…",
	"energy-redistribution-path-walking…",
	"gradient-domain-rendering…",
	"wavelet-noise-turbulence-layering…",
	"perlin-noise-value-interpolating…",
	"simplex-noise-gradient-summing…",
	"worley-noise-cell-distance-computing…",
	"fbm-octave-lacunarity-scaling…",
	"domain-warping-distorting…",
	"fractal-brownian-motion-summing…",
	"tiling-noise-seamless-wrapping…",
	"binary-space-partition-cutting…",
	"portal-rendering-the room…",
	"mirror-rendering-reflecting…",
	"water-rendering-caustic-projecting…",
	"hair-rendering-fiber-clumping…",
	"fur-rendering-shell-layering…",
	"skin-rendering-SSS-diffusing…",
	"eye-rendering-cornea-refracting…",
	"cloth-rendering-anisotropy-scattering…",
	"leather-rendering-grain-bump-mapping…",
	"metal-rendering-Fresnel-reflecting…",
	"glass-rendering-transparency-refracting…",
	"gem-rendering-dispersion-splitting…",
	"atmosphere-rendering-rayleigh-scattering…",
	"cloud-rendering-volume-density-marching…",
	"smoke-rendering-particle-illuminating…",
	"fire-rendering-flame-dynamic…",
	"explosion-rendering-plasma-advecting…",
	"lightning-rendering-branch-path-tracing…",
	"nebula-rendering-emission-nebulous…",
	"galaxy-rendering-spiral-density-waving…",
	"black-hole-rendering-accretion-disk…",
	"wormhole-rendering-einstein-rosen-bridging…",
	"time-dilation-relative-rendering…",
	"quantum-rendering-probability-cloud…",
	"string-theory-calabi-yau-manifolding…",
}

// Config holds dependencies for creating the TUI model.
type Config struct {
	Provider       llm.Provider
	Session        *session.Session
	Mode           string
	Scope          string
	Model          string
	Override       string
	Registry       *tools.Registry
	MaxToolIter    int
	ShellEnabled   bool
	Fullscreen     bool
	ThemeName      string
	ScopeInfo      ScopeInfo
	GlamourEnabled bool
	ScrollLines    int
	FollowMode     bool
	MemoryStore    memory.Store
	Retriever      *memory.Retriever
	Compressor     *ctxcomp.Compressor
	ModelInfo      llm.ModelInfo

	// Phase 19: navigation config
	Shell         string
	PermLevel     string
	LeaderKeyStr  string
	LeaderTimeout int
	HistorySize   int

	// ToolTimeout is the default timeout in seconds for tool calls.
	ToolTimeout int

	// Phase 20: logging config
	LogEnabled           bool
	LogDefaultLevel      string
	LogMaxEntries        int
	LogDisplayLimit      int
	LogCollapse          bool
	LogCollapseThreshold int
	LogChan              chan LogEntry
	LogHandler           *TUILogHandler

	// Phase 26: per-tool permission rule sets
	ToolRules map[string]*tools.RuleSet
}

// NewModel creates a Bubbletea model ready to run.
func NewModel(cfg Config) Model {
	theme := styles.GetTheme(cfg.ThemeName)
	vp := tuivp.New(80, 20)

	ta := textarea.New()
	ta.Placeholder = ""
	ta.ShowLineNumbers = false
	ta.SetHeight(1)
	ta.SetWidth(80)
	ta.Focus()

	ta.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("shift+enter", "ctrl+j"),
		key.WithHelp("shift+enter", "newline"),
	)

	if !cfg.FollowMode {
		vp.SetFollowMode(false)
	}
	scrollLines := cfg.ScrollLines
	if scrollLines <= 0 {
		scrollLines = 5
	}
	histSize := cfg.HistorySize
	if histSize <= 0 {
		histSize = 100
	}
	leaderTimeout := time.Duration(cfg.LeaderTimeout) * time.Millisecond
	if leaderTimeout <= 0 {
		leaderTimeout = 2 * time.Second
	}

	ms := navigation.NewModeSwitcher()
	if cfg.Mode != "" {
		ms.SetCurrent(cfg.Mode)
	}

	lk := keybinds.NewLeaderKey(cfg.LeaderKeyStr, leaderTimeout)
	lk.RegisterBinding("c", keybinds.ActionCompact)
	lk.RegisterBinding("n", keybinds.ActionNew)
	lk.RegisterBinding("l", keybinds.ActionList)
	lk.RegisterBinding("m", keybinds.ActionModel)
	lk.RegisterBinding("t", keybinds.ActionTheme)
	lk.RegisterBinding("a", keybinds.ActionAgent)
	lk.RegisterBinding("u", keybinds.ActionUndo)
	lk.RegisterBinding("r", keybinds.ActionRedo)
	lk.RegisterBinding("e", keybinds.ActionEditor)
	lk.RegisterBinding("x", keybinds.ActionExport)
	lk.RegisterBinding("q", keybinds.ActionQuit)
	lk.RegisterBinding("s", keybinds.ActionStatus)
	lk.RegisterBinding("b", keybinds.ActionSidebar)
	lk.RegisterBinding("h", keybinds.ActionTips)
	lk.RegisterBinding("y", keybinds.ActionCopy)

	h := history.New(histSize)
	cp := palette.New()
	cp.AddItem(palette.CommandItem{
		Name: "quit", Slash: "/quit", Description: "Exit shmorby",
	})
	cp.AddItem(palette.CommandItem{
		Name: "reset", Slash: "/reset", Description: "Clear session history",
	})
	cp.AddItem(palette.CommandItem{
		Name: "model", Slash: "/model", Description: "Show current model",
	})
	cp.AddItem(palette.CommandItem{
		Name: "agent", Slash: "/agent", Description: "Switch agent mode",
	})
	cp.AddItem(palette.CommandItem{
		Name: "scope", Slash: "/scope", Description: "Show scope files",
	})
	cp.AddItem(palette.CommandItem{
		Name: "memory", Slash: "/memory", Description: "Memory management",
	})
	cp.AddItem(palette.CommandItem{
		Name: "context", Slash: "/context", Description: "Context stats",
	})
	cp.AddItem(palette.CommandItem{
		Name: "help", Slash: "/help", Description: "Show help",
	})

	// Parse default log level.
	logLevel := slog.LevelInfo
	switch cfg.LogDefaultLevel {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	}

	logChan := cfg.LogChan
	if logChan == nil {
		logChan = make(chan LogEntry, 100)
	}

	logMax := cfg.LogMaxEntries
	if logMax <= 0 {
		logMax = 100
	}
	logDisplay := cfg.LogDisplayLimit
	if logDisplay <= 0 {
		logDisplay = 20
	}
	logThreshold := cfg.LogCollapseThreshold
	if logThreshold <= 0 {
		logThreshold = 5
	}

	return Model{
		textarea:        ta,
		viewport:        vp,
		theme:           theme,
		provider:        cfg.Provider,
		session:         cfg.Session,
		mode:            cfg.Mode,
		scope:           cfg.Scope,
		model:           cfg.Model,
		override:        cfg.Override,
		registry:        cfg.Registry,
		maxIter:         cfg.MaxToolIter,
		shell:           cfg.ShellEnabled,
		fullscreen:      cfg.Fullscreen,
		scopeInfo:       cfg.ScopeInfo,
		complEngine:     tuicompl.New(),
		glamourEnabled:  cfg.GlamourEnabled,
		scrollLines:     scrollLines,
		memoryStore:     cfg.MemoryStore,
		retriever:       cfg.Retriever,
		compressor:      cfg.Compressor,
		modelInfo:       cfg.ModelInfo,
		modeSwitcher:    ms,
		referenceEngine: navigation.NewReferenceEngine(),
		shellCmdHandler: navigation.NewShellCmdHandler(
			navigation.OSExecutor{}, cfg.Shell, cfg.Mode, cfg.PermLevel,
		),
		scrollAccel:          navigation.NewScrollAcceleration(),
		leaderKey:            lk,
		whichKey:             keybinds.NewWhichKey(cfg.LeaderKeyStr),
		commandPalette:       cp,
		inputHistory:         h,
		reverseSearch:        history.NewReverseISearch(h),
		tabBar:               sessiontab.New("default", "default"),
		logChan:              logChan,
		logLevel:             logLevel,
		logExpanded:          !cfg.LogCollapse,
		logMaxEntries:        logMax,
		logDisplayLimit:      logDisplay,
		logCollapse:          cfg.LogCollapse,
		logCollapseThreshold: logThreshold,
		logHandler:           cfg.LogHandler,
		agentEventChan:       make(chan agent.AgentEvent, 20),
		permissionReqChan:    make(chan PermissionPrompt),
		toolRules:            cfg.ToolRules,
		showHelp:             NewHelpModel(),
	}
}

// Init returns the initial command (no-op) plus a log listener when
// the log channel is configured.
func (m Model) Init() tea.Cmd {
	var cmds []tea.Cmd
	cmds = append(cmds, tea.HideCursor)
	if m.logChan != nil {
		cmds = append(cmds, m.listenLogChan())
	}
	cmds = append(cmds, m.listenAgentEvents())
	cmds = append(cmds, m.listenPermissionReqs())
	return tea.Batch(cmds...)
}

// listenLogChan reads from the log channel and returns entries as
// bubbletea messages. Returns a follow-up command to keep listening.
func (m Model) listenLogChan() tea.Cmd {
	return func() tea.Msg {
		entry, ok := <-m.logChan
		if !ok {
			return nil
		}
		return logEntryMsg{entry: entry}
	}
}

// listenAgentEvents reads from the agent event channel and returns
// events as bubbletea messages. Returns a follow-up command.
func (m Model) listenAgentEvents() tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-m.agentEventChan
		if !ok {
			return nil
		}
		return agentEventMsg{event: ev}
	}
}

// listenPermissionReqs reads from the permission request channel and
// returns prompts as bubbletea messages. Returns a follow-up command.
func (m Model) listenPermissionReqs() tea.Cmd {
	return func() tea.Msg {
		prompt, ok := <-m.permissionReqChan
		if !ok {
			return nil
		}
		return permissionReqMsg{prompt: prompt}
	}
}

// Update handles incoming messages and key events.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.SetWidth(msg.Width)
		m.textarea.SetWidth(msg.Width)
		m.textarea.SetHeight(m.inputLineHeight())
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		m.viewport.MouseMsg(msg)
		// Sync selection mode from viewport (click enters selection mode).
		if m.viewport.SelectionMode() && !m.selectionMode {
			m.selectionMode = true
			m.textarea.Blur()
		}
		// Sync drag selection continuously for highlighting.
		if m.selectionMode {
			start, end, active := m.viewport.DragSelection()
			if active || m.viewport.IsDragging() {
				m.selectionStart = start
				m.selectionEnd = end
				m.syncViewport()
			}
		}
		return m, nil

	case submitMsg:
		return m.handleSubmit(msg.text)

	case agentReplyMsg:
		m.running = false
		m.currentTool = ""
		m.currentToolStatus = ""
		m.updateCtxStats()
		// Estimate token count from reply length (~4 chars/token).
		m.tokensDown = len(msg.text) / 4
		m.textarea.Reset()
		text := msg.text
		if m.glamourEnabled {
			text = tuirender.RenderMarkdown(text, m.width-2)
		}
		m.output = append(m.output, outputEntry{
			kind: "agent",
			text: text,
		})
		// Show memory indicator when memory was used.
		if msg.memoryEntries > 0 {
			memoryIndicator := fmt.Sprintf(
				"[memory: %d entries]",
				msg.memoryEntries,
			)
			m.output = append(m.output, outputEntry{
				kind: "memory",
				text: memoryIndicator,
			})
		}
		m.syncViewport()
		// Delay spinner stop so user sees output before spinner
		// disappears (prevents "frozen" perception).
		return m, tea.Batch(
			tea.Tick(
				200*time.Millisecond,
				func(_ time.Time) tea.Msg {
					return spinnerStopMsg{}
				},
			),
		)

	case settleMsg:
		return m.handleSettle()

	case spinnerStopMsg:
		m.spinner.Stop()
		return m, nil

	case agentModeChangedMsg:
		m.mode = msg.mode
		m.output = append(m.output, outputEntry{
			kind: "agent",
			text: fmt.Sprintf("Switched to %s mode.", msg.mode),
		})
		m.syncViewport()
		return m, nil

	case leaderTimeoutMsg:
		if m.leaderKey.Active() {
			if time.Now().After(m.leaderKey.Deadline()) {
				m.leaderKey.Deactivate()
				m.whichKey.Dismiss()
			} else {
				return m, tea.Tick(
					m.leaderKey.Deadline().Sub(time.Now()),
					func(_ time.Time) tea.Msg {
						return leaderTimeoutMsg{}
					},
				)
			}
		}
		return m, nil

	case logEntryMsg:
		m.logEntries = append(m.logEntries, msg.entry)
		if len(m.logEntries) > m.logMaxEntries {
			m.logEntries = m.logEntries[len(m.logEntries)-m.logMaxEntries:]
		}
		if len(m.logEntries) > m.logCollapseThreshold {
			m.logExpanded = false
		}
		m.syncViewport()
		return m, m.listenLogChan()

	case thinkingDeltaMsg:
		m.thinking.AddDelta(msg.delta)
		m.syncViewport()
		return m, nil

	case thinkingEndMsg:
		m.thinking.End()
		m.syncViewport()
		return m, nil

	case setLogLevelMsg:
		m.logLevel = msg.level
		if m.logHandler != nil {
			m.logHandler.SetLevel(msg.level)
		}
		m.output = append(m.output, outputEntry{
			kind: "agent",
			text: fmt.Sprintf("Log level: %s", msg.level),
		})
		m.syncViewport()
		return m, nil

	case permissionReqMsg:
		m.permission = &msg.prompt
		return m, m.listenPermissionReqs()

	case agentEventMsg:
		switch msg.event.Type {
		case "tool-start":
			m.currentTool = msg.event.Name
			m.currentToolStatus = msg.event.Info
			m.spinner.Start(waitingMessages[rand.Intn(len(waitingMessages))])
			m.output = append(m.output, outputEntry{
				kind: "tool",
				text: fmt.Sprintf(
					"$ %s", msg.event.Info,
				),
			})
			m.syncViewport()
		case "tool-end":
			m.spinner.Start(thinkingMessages[rand.Intn(len(thinkingMessages))])
			m.output = append(m.output, outputEntry{
				kind: "tool",
				text: fmt.Sprintf(
					"%s: %s", msg.event.Name, msg.event.Info,
				),
			})
			if msg.event.Output != "" {
				m.output = append(m.output, outputEntry{
					kind: "agent",
					text: msg.event.Output,
				})
			}
			m.currentTool = ""
			m.currentToolStatus = ""
			m.syncViewport()
		}
		return m, m.listenAgentEvents()

	case outputMsg:
		m.output = append(m.output, outputEntry{
			kind: "agent",
			text: msg.text,
		})
		m.syncViewport()
		return m, nil

	case streamDeltaMsg:
		if m.settleTimer != nil {
			m.settleTimer.Stop()
			m.settleTimer = nil
		}
		lines := m.streamBuf.WriteToken(msg.delta)
		m.tokensDown = m.streamBuf.Tokens()
		for _, line := range lines {
			m.output = append(m.output, outputEntry{
				kind: "agent",
				text: line,
			})
		}
		m.syncViewport()
		return m, nil

	case streamDoneMsg:
		// Defer final render until stream settles (50ms since last delta).
		if m.streamBuf.SettleElapsed() < 50*time.Millisecond {
			m.settleTimer = time.NewTimer(
				50*time.Millisecond - m.streamBuf.SettleElapsed(),
			)
			return m, func() tea.Msg {
				<-m.settleTimer.C
				return settleMsg{}
			}
		}
		return m.finalizeStream()

	case toolStatusMsg:
		m.currentTool = msg.name
		m.currentToolStatus = msg.status
		m.output = append(m.output, outputEntry{
			kind: "tool",
			text: fmt.Sprintf("%s: %s", msg.name, msg.status),
		})
		m.syncViewport()
		return m, nil

	case errorMsg:
		m.running = false
		m.currentTool = ""
		m.currentToolStatus = ""
		m.output = append(m.output, outputEntry{
			kind: "error",
			text: msg.err.Error(),
		})
		m.syncViewport()
		return m, tea.Batch(
			tea.Tick(
				200*time.Millisecond,
				func(_ time.Time) tea.Msg {
					return spinnerStopMsg{}
				},
			),
		)

	case spinnerTickMsg:
		if m.running {
			m.spinner.Tick()
			return m, tea.Tick(
				100*time.Millisecond,
				func(_ time.Time) tea.Msg { return spinnerTickMsg{} },
			)
		}
		return m, nil

	case permissionResultMsg:
		if m.permission != nil {
			m.permission.Choice <- msg.choice
			m.permission = nil
		}
		if m.pendingClearMemory {
			m.pendingClearMemory = false
			if msg.choice == PermissionAllow {
				m.executeMemoryClear()
			} else {
				m.output = append(m.output, outputEntry{
					kind: "agent",
					text: "Memory clear cancelled.",
				})
				m.syncViewport()
			}
		}
		return m, nil
	}

	return m, nil
}

// handleKey processes keyboard input.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Halt-all prompt active: only y/n/esc.
	if m.haltPrompt {
		switch msg.String() {
		case "y":
			m.haltPrompt = false
			if m.cancel != nil {
				m.cancel()
			}
			m.running = false
			m.spinner.Stop()
			m.currentTool = ""
			m.currentToolStatus = ""
			m.output = append(m.output, outputEntry{
				kind: "agent",
				text: "All operations halted.",
			})
			m.syncViewport()
			return m, nil
		case "n", "esc":
			m.haltPrompt = false
			return m, nil
		}
		return m, nil
	}

	// Help overlay active: scroll or close.
	if m.showHelp != nil && m.showHelp.Visible() {
		switch msg.Type {
		case tea.KeyPgUp:
			m.showHelp.ScrollUp()
			return m, nil
		case tea.KeyPgDown:
			m.showHelp.ScrollDown(30, m.height-4)
			return m, nil
		case tea.KeyUp:
			m.showHelp.ScrollUp()
			return m, nil
		case tea.KeyDown:
			m.showHelp.ScrollDown(30, m.height-4)
			return m, nil
		case tea.KeyEsc, tea.KeyEnter:
			m.showHelp.Hide()
			return m, nil
		default:
			m.showHelp.Hide()
			return m, nil
		}
	}

	// Permission prompt active: only y/n/a/esc.
	if m.permission != nil {
		switch msg.String() {
		case "y":
			return m, func() tea.Msg {
				return permissionResultMsg{choice: PermissionAllow}
			}
		case "n":
			return m, func() tea.Msg {
				return permissionResultMsg{choice: PermissionDeny}
			}
		case "a":
			return m, func() tea.Msg {
				return permissionResultMsg{choice: PermissionAllowAll}
			}
		case "esc":
			return m, func() tea.Msg {
				return permissionResultMsg{choice: PermissionDeny}
			}
		}
		return m, nil
	}

	// Clipboard keybindings.
	s := msg.String()
	switch s {
	case "ctrl+c":
		var copied bool
		if m.selectionMode && m.selectionStart != m.selectionEnd {
			lines := outputTexts(m.output)
			start, end := m.selectionStart, m.selectionEnd
			if start > end {
				start, end = end, start
			}
			if start < len(lines) {
				if end > len(lines) {
					end = len(lines)
				}
				raw := strings.Join(lines[start:end], "\n")
				tuicl.Copy(StripANSI(raw))
				copied = true
			}
		} else if len(m.output) > 0 {
			// Copy last agent reply when no selection active.
			for i := len(m.output) - 1; i >= 0; i-- {
				if m.output[i].kind == "agent" {
					tuicl.Copy(StripANSI(m.output[i].text))
					copied = true
					break
				}
			}
		}
		if copied {
			m.copyNotify = "✓ copied to clipboard"
			m.copyNotifyTime = time.Now()
		}
		return m, nil
	case "ctrl+v":
		pasteText := tuicl.Paste()
		if pasteText != "" {
			val := m.textarea.Value()
			pos := len(val)
			m.textarea.SetValue(val + pasteText)
			m.textarea.SetCursor(pos + len(pasteText))
		}
		return m, nil
	case "ctrl+shift+pgdown":
		m.viewport.GotoBottom()
		return m, nil
	case "ctrl+shift+pgup":
		m.viewport.GotoTop()
		return m, nil
	}

	// Selection mode active: only esc to exit.
	if m.selectionMode {
		switch msg.Type {
		case tea.KeyEsc, tea.KeyEnter:
			m.selectionMode = false
			m.viewport.SetSelectionMode(false)
			m.textarea.Focus()
			m.selectionStart = 0
			m.selectionEnd = 0
			return m, nil
		}
		return m, nil
	}

	// Command palette active: route keys to palette filter.
	if m.commandPalette.Visible() {
		return m.handlePaletteKey(msg)
	}

	// Reverse search active: route keys to search filter.
	if m.reverseSearch.Visible() {
		return m.handleReverseSearchKey(msg)
	}

	switch msg.Type {
	case tea.KeyCtrlC:
		if m.cancel != nil {
			m.cancel()
		}
		return m, tea.Quit

	case tea.KeyEnter:
		if m.running {
			return m, nil
		}
		if m.showCompletion && len(m.complMatches) > 0 {
			sel := m.complMatches[m.complIdx]
			m.textarea.SetValue(sel.Name + " ")
			m.textarea.SetCursor(len(sel.Name) + 1)
			m.showCompletion = false
			m.complMatches = nil
			return m, nil
		}
		text := strings.TrimSpace(m.textarea.Value())
		if text == "" {
			return m, nil
		}
		m.textarea.Reset()
		m.inputHistory.Add(text)
		m.showCompletion = false
		m.complMatches = nil
		return m, func() tea.Msg { return submitMsg{text: text} }

	case tea.KeyUp:
		if m.showCompletion && len(m.complMatches) > 0 {
			m.complIdx--
			if m.complIdx < 0 {
				m.complIdx = len(m.complMatches) - 1
			}
			return m, nil
		}
		if m.textarea.Value() != "" || m.inputHistory.Size() == 0 {
			var cmd tea.Cmd
			m.textarea, cmd = m.textarea.Update(msg)
			return m, cmd
		}
		if entry, ok := m.inputHistory.Older(); ok {
			m.textarea.SetValue(entry)
		}
		return m, nil

	case tea.KeyDown:
		if m.showCompletion && len(m.complMatches) > 0 {
			m.complIdx++
			if m.complIdx >= len(m.complMatches) {
				m.complIdx = 0
			}
			return m, nil
		}
		// Empty input or cursor not at end → advance history.
		if m.textarea.Value() == "" || !m.inputHistory.AtNewest() {
			if entry, ok := m.inputHistory.Newer(); ok {
				if entry != "" {
					m.textarea.SetValue(entry)
				} else {
					m.textarea.Reset()
				}
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		return m, cmd

	case tea.KeyTab:
		if m.showCompletion && len(m.complMatches) > 0 {
			sel := m.complMatches[m.complIdx]
			m.textarea.SetValue(sel.Name + " ")
			m.textarea.SetCursor(len(sel.Name) + 1)
			m.showCompletion = false
			m.complMatches = nil
			return m, nil
		}
		// Empty input → cycle agent mode forward (Tab).
		if m.textarea.Value() == "" && !m.running {
			m.modeSwitcher.CycleForward()
			m.mode = m.modeSwitcher.Current()
			m.shellCmdHandler.SetMode(m.mode)
			return m, func() tea.Msg {
				return agentModeChangedMsg{mode: m.mode}
			}
		}
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		return m, cmd

	case tea.KeyShiftTab:
		// Shift+Tab → cycle agent mode reverse.
		if m.textarea.Value() == "" && !m.running {
			m.modeSwitcher.CycleReverse()
			m.mode = m.modeSwitcher.Current()
			m.shellCmdHandler.SetMode(m.mode)
			return m, func() tea.Msg {
				return agentModeChangedMsg{mode: m.mode}
			}
		}
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		return m, cmd

	case tea.KeyEsc:
		if m.showCompletion {
			m.showCompletion = false
			m.complMatches = nil
			return m, nil
		}
		if m.leaderKey.Active() {
			m.leaderKey.Deactivate()
			m.whichKey.Dismiss()
			return m, nil
		}
		if m.commandPalette.Visible() {
			m.commandPalette.Dismiss()
			return m, nil
		}
		if m.reverseSearch.Visible() {
			m.reverseSearch.Dismiss()
			m.showReverseSearch = false
			return m, nil
		}
		if m.running && !m.haltPrompt {
			m.haltPrompt = true
			return m, nil
		}
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		return m, cmd

	case tea.KeyCtrlP:
		// Command palette toggle.
		m.commandPalette.Toggle()
		return m, nil

	case tea.KeyCtrlL:
		// Toggle log section visibility.
		m.logExpanded = !m.logExpanded
		m.syncViewport()
		return m, nil

	case tea.KeyCtrlT:
		// Toggle thinking block visibility.
		m.thinkingExpanded = !m.thinkingExpanded
		m.syncViewport()
		return m, nil

	case tea.KeyCtrlH:
		// Toggle help overlay.
		if m.showHelp != nil {
			m.showHelp.Toggle()
		}
		return m, nil

	case tea.KeyCtrlR:
		// Reverse-i-search toggle.
		m.reverseSearch.Toggle()
		m.showReverseSearch = m.reverseSearch.Visible()
		return m, nil

	case tea.KeyCtrlS:
		// Reverse-i-search backward cycle when active.
		if m.reverseSearch.Visible() {
			m.reverseSearch.CycleReverse()
			return m, nil
		}
		return m, nil

	case tea.KeyPgUp:
		n := 1
		if m.scrollAccel != nil && m.scrollAccel.Enabled() {
			n = int(m.scrollAccel.Tick())
		}
		for i := 0; i < n; i++ {
			m.viewport.ScrollHalfPageUp()
		}
		return m, nil

	case tea.KeyPgDown:
		n := 1
		if m.scrollAccel != nil && m.scrollAccel.Enabled() {
			n = int(m.scrollAccel.Tick())
		}
		for i := 0; i < n; i++ {
			m.viewport.ScrollHalfPageDown()
		}
		return m, nil

	case tea.KeyRunes:
		// Leader key active → dispatch second key.
		if m.leaderKey.Active() {
			action, consumed := m.leaderKey.HandleKey(s)
			m.whichKey.Dismiss()
			if !consumed {
				return m, nil
			}
			return m.dispatchLeaderAction(action)
		}
		// @-reference trigger: detect @ at cursor position.
		if len(msg.Runes) == 1 && msg.Runes[0] == '@' {
			var cmd tea.Cmd
			m.textarea, cmd = m.textarea.Update(msg)
			m.updateCompletion()
			return m, cmd
		}
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		m.updateCompletion()
		return m, cmd

	default:
		// Leader key detection.
		if m.leaderKey.IsLeaderKey(s) {
			m.leaderKey.Activate()
			m.whichKey.Show(
				m.leaderKey.BindingsList(),
				m.width,
				m.leaderKey.Timeout,
			)
			return m, func() tea.Msg {
				return leaderTimeoutMsg{}
			}
		}
		// Delegate to textarea, then check for completion trigger.
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		m.updateCompletion()
		return m, cmd
	}
}

// dispatchLeaderAction executes the action mapped by a leader-key binding.
func (m Model) dispatchLeaderAction(
	action keybinds.Action,
) (tea.Model, tea.Cmd) {
	switch action {
	case keybinds.ActionCompact:
		return m, func() tea.Msg {
			return submitMsg{text: "/context compact"}
		}
	case keybinds.ActionNew:
		return m, func() tea.Msg {
			return submitMsg{text: "new session"}
		}
	case keybinds.ActionModel:
		return m, func() tea.Msg {
			return submitMsg{text: "/model"}
		}
	case keybinds.ActionAgent:
		m.modeSwitcher.CycleForward()
		m.mode = m.modeSwitcher.Current()
		m.shellCmdHandler.SetMode(m.mode)
		return m, nil
	case keybinds.ActionQuit:
		return m, tea.Quit
	case keybinds.ActionStatus:
		return m, func() tea.Msg {
			return submitMsg{text: "/scope"}
		}
	default:
		return m, nil
	}
}

// updateCompletion checks the current input and updates completion state.
func (m *Model) updateCompletion() {
	val := m.textarea.Value()
	if strings.HasPrefix(val, "/") {
		matches := m.complEngine.Complete(val)
		if len(matches) > 0 {
			m.complMatches = matches
			m.complIdx = 0
			m.showCompletion = true
			return
		}
	}
	// @-reference completion.
	if idx := strings.LastIndex(val, "@"); idx >= 0 {
		query := val[idx+1:]
		if idx == 0 || val[idx-1] == ' ' {
			items := m.referenceEngine.Complete(query)
			if len(items) > 0 {
				m.complMatches = nil
				for _, item := range items {
					m.complMatches = append(m.complMatches, tuicompl.Command{
						Name:        item.Label,
						Description: item.Kind,
					})
				}
				m.complIdx = 0
				m.showCompletion = true
				return
			}
		}
	}
	m.showCompletion = false
	m.complMatches = nil
}

// handleSubmit processes a user message by running the agent turn.
func (m Model) handleSubmit(text string) (tea.Model, tea.Cmd) {
	// !-prefixed shell commands run outside the agent loop.
	if handled, out, err := m.shellCmdHandler.Handle(text); handled {
		m.output = append(m.output, outputEntry{
			kind: "user",
			text: text,
		})
		if err != nil {
			m.output = append(m.output, outputEntry{
				kind: "error",
				text: err.Error(),
			})
		} else {
			result := navigation.FormatOutput(out)
			if result != "" {
				m.output = append(m.output, outputEntry{
					kind: "agent",
					text: result,
				})
			}
		}
		m.syncViewport()
		return m, nil
	}

	// Resolve @-references in the message before sending to agent.
	text, refContent := m.resolveReferences(text)

	m.output = append(m.output, outputEntry{
		kind: "user",
		text: text,
	})
	m.syncViewport()

	if cmd, done, err := m.handleCommand(text); done {
		return m, tea.Quit
	} else if err != nil {
		return m, func() tea.Msg {
			return outputMsg{text: fmt.Sprintf("Error: %v", err)}
		}
	} else if cmd {
		return m, nil
	}

	// Prepend resolved reference content as context for the agent.
	if refContent != "" {
		text = refContent + "\n\n" + text
	}

	m.running = true
	m.startTime = time.Now()
	m.tokensDown = 0
	m.streamBuf = NewStreamBuffer()
	m.spinner.Start(thinkingMessages[rand.Intn(len(thinkingMessages))])

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel

	return m, tea.Batch(
		m.runAgentTurn(ctx, text),
		func() tea.Msg { return spinnerTickMsg{} },
		tea.HideCursor,
	)
}

// handleCommand processes slash commands. Returns (handled, shouldQuit, error).
func (m *Model) handleCommand(line string) (bool, bool, error) {
	if !strings.HasPrefix(line, "/") {
		return false, false, nil
	}

	parts := strings.Fields(line)
	if len(parts) == 0 {
		return false, false, nil
	}

	switch parts[0] {
	case "/quit":
		return true, true, nil

	case "/reset":
		m.session.Reset()
		return true, false, nil

	case "/model":
		providerName := "none"
		if m.provider != nil {
			providerName = m.provider.Name()
		}
		m.output = append(m.output, outputEntry{
			kind: "agent",
			text: fmt.Sprintf("%s (%s)", providerName, m.model),
		})
		m.syncViewport()
		return true, false, nil

	case "/agent":
		if len(parts) == 2 {
			if m.modeSwitcher.SetCurrent(parts[1]) {
				m.mode = parts[1]
				m.shellCmdHandler.SetMode(m.mode)
				m.output = append(m.output, outputEntry{
					kind: "agent",
					text: fmt.Sprintf("Switched to %s mode.", m.mode),
				})
				m.syncViewport()
				return true, false, nil
			}
			return true, false, fmt.Errorf("unknown agent mode: %s", parts[1])
		}
		m.output = append(m.output, outputEntry{
			kind: "agent",
			text: m.mode,
		})
		m.syncViewport()
		return true, false, nil

	case "/scope":
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("scope: %d bytes", m.scopeInfo.TotalBytes))
		if m.scopeInfo.PrimaryPath != "" {
			sb.WriteString(fmt.Sprintf("\n  primary: %s", m.scopeInfo.PrimaryPath))
		}
		for _, inst := range m.scopeInfo.Instructions {
			sb.WriteString(fmt.Sprintf("\n  instruction: %s", inst))
		}
		text := sb.String()
		m.output = append(m.output, outputEntry{
			kind: "agent",
			text: text,
		})
		m.syncViewport()
		return true, false, nil

	case "/tui":
		m.fullscreen = !m.fullscreen
		m.output = append(m.output, outputEntry{
			kind: "agent",
			text: fmt.Sprintf("Fullscreen: %v", m.fullscreen),
		})
		m.syncViewport()
		return true, false, nil

	case "/memory":
		m.handleMemoryCommand(parts)
		return true, false, nil

	case "/context":
		args := strings.TrimPrefix(line, "/context")
		args = strings.TrimSpace(args)
		m.handleContextCommand(args)
		return true, false, nil

	case "/help":
		if m.showHelp != nil {
			m.showHelp.Show()
		}
		return true, false, nil

	case "/log":
		if len(parts) == 2 {
			var lvl slog.Level
			switch parts[1] {
			case "debug":
				lvl = slog.LevelDebug
			case "info":
				lvl = slog.LevelInfo
			case "warn":
				lvl = slog.LevelWarn
			case "error":
				lvl = slog.LevelError
			default:
				return true, false, fmt.Errorf(
					"unknown log level: %s (want debug|info|warn|error)",
					parts[1],
				)
			}
			m.logLevel = lvl
			if m.logHandler != nil {
				m.logHandler.SetLevel(lvl)
			}
			m.output = append(m.output, outputEntry{
				kind: "agent",
				text: fmt.Sprintf("Log level: %s", lvl),
			})
			m.syncViewport()
			return true, false, nil
		}
		m.output = append(m.output, outputEntry{
			kind: "agent",
			text: fmt.Sprintf("Log level: %s", m.logLevel),
		})
		m.syncViewport()
		return true, false, nil

	default:
		return true, false, fmt.Errorf("unknown command: %s", parts[0])
	}
}

// runAgentTurn executes the agent in a goroutine and sends the reply.
// Uses streaming when the provider supports it and no tools are needed.
func (m Model) runAgentTurn(
	ctx context.Context, input string,
) tea.Cmd {
	return func() tea.Msg {
		// Track memory stats before the turn.
		var prevHits int
		if m.retriever != nil {
			prevHits = m.retriever.Stats().Hits
		}

		if m.registry == nil {
			events, err := m.provider.ChatStream(ctx, llm.ChatRequest{
				Model:  m.model,
				System: m.buildSystemPrompt(),
				Messages: []llm.Message{
					{Role: "user", Content: input},
				},
			})
			if err == nil {
				return m.consumeStream(events)
			}
		}

		var reply string
		var err error
		if m.registry != nil {
			// Only wire permission func when interactive rules
			// are configured (permission.interactive: true).
			var permFunc agent.ToolPermissionFunc
			if m.toolRules != nil {
				permFunc = m.toolPermissionFunc
			}
			reply, err = agent.RunTurnWithTools(
				ctx, m.provider, m.session,
				m.mode, m.scope, m.override, m.model, input,
				m.registry, m.maxIter, m.shell,
				m.memoryStore, m.retriever,
				m.compressor, m.modelInfo,
				func(ev agent.AgentEvent) {
					select {
					case m.agentEventChan <- ev:
					default:
					}
				},
				permFunc,
				m.toolRules,
			)
		} else {
			reply, err = agent.RunTurn(
				ctx, m.provider, m.session,
				m.mode, m.scope, m.override, m.model, input,
				m.memoryStore, m.retriever,
				m.compressor, m.modelInfo,
			)
		}
		if err != nil {
			return errorMsg{err: err}
		}

		// Calculate memory entries used in this turn.
		var memoryEntries int
		if m.retriever != nil {
			currentHits := m.retriever.Stats().Hits
			if currentHits > prevHits {
				memoryEntries = m.retriever.Stats().LastCount
			}
		}

		return agentReplyMsg{text: reply, memoryEntries: memoryEntries}
	}
}

// consumeStream reads SSE events and converts them to tea.Msg values.
// It tracks whether reasoning deltas have been seen so that when a text
// delta arrives, the thinking block is finalized first.
func (m Model) consumeStream(
	events <-chan llm.StreamEvent,
) tea.Cmd {
	wasReasoning := false
	pendingText := ""
	return func() tea.Msg {
		// Return buffered text delta from a previous call.
		if pendingText != "" {
			d := pendingText
			pendingText = ""
			return streamDeltaMsg{delta: d}
		}
		for ev := range events {
			switch {
			case ev.Done:
				return streamDoneMsg{}
			case ev.Type == "reasoning" && ev.Delta != "":
				wasReasoning = true
				return thinkingDeltaMsg{delta: ev.Delta}
			case ev.Type == "text" && ev.Delta != "":
				if wasReasoning {
					wasReasoning = false
					pendingText = ev.Delta
					return thinkingEndMsg{}
				}
				return streamDeltaMsg{delta: ev.Delta}
			case ev.Type == "error":
				return errorMsg{
					err: fmt.Errorf("stream: %s", ev.Delta),
				}
			}
		}
		return streamDoneMsg{}
	}
}

// toolPermissionFunc implements agent.ToolPermissionFunc for the TUI.
// Sends a permission prompt to the TUI update loop and blocks until
// the user responds.
func (m *Model) toolPermissionFunc(toolName, command, reason string) agent.ToolPermissionResponse {
	prompt := NewPermissionPrompt(toolName, command, reason, "tool permission")
	m.permissionReqChan <- prompt

	choice := <-prompt.Choice

	switch choice {
	case PermissionAllowAll:
		return agent.PermAllowAll
	case PermissionAllow:
		return agent.PermAllow
	default:
		return agent.PermDeny
	}
}

// buildSystemPrompt returns the system prompt for streaming requests.
func (m Model) buildSystemPrompt() string {
	if m.override != "" {
		return m.override
	}
	return fmt.Sprintf(
		"You are in %s mode. %s",
		m.mode, m.scope,
	)
}

// finalizeStream renders the final accumulated content when streaming ends.
func (m Model) finalizeStream() (tea.Model, tea.Cmd) {
	// End any active thinking block.
	if m.thinking.Active() {
		m.thinking.End()
	}

	remaining := m.streamBuf.Flush()
	if remaining != "" {
		text := remaining
		if m.glamourEnabled {
			text = tuirender.RenderMarkdown(remaining, m.width-2)
		}
		m.output = append(m.output, outputEntry{
			kind: "agent",
			text: text,
		})
	}
	m.tokensDown = m.streamBuf.Tokens()
	m.running = false
	m.currentTool = ""
	m.currentToolStatus = ""
	m.textarea.Reset()
	m.updateCtxStats()
	m.syncViewport()
	// Delay spinner stop so user sees output first.
	return m, tea.Batch(
		tea.Tick(
			200*time.Millisecond,
			func(_ time.Time) tea.Msg {
				return spinnerStopMsg{}
			},
		),
	)
}

// handleSettle processes the settle timer expiry and finalizes the stream.
func (m Model) handleSettle() (tea.Model, tea.Cmd) {
	m.settleTimer = nil
	return m.finalizeStream()
}

// syncViewport updates the viewport with current output.
func (m *Model) syncViewport() {
	m.ensureLayout()
	var sb strings.Builder
	for i, entry := range m.output {
		selected := m.selectionMode &&
			i >= m.selectionStart && i < m.selectionEnd
		// Strip incomplete ANSI sequences before styling.
		text := StripPartialANSI(entry.text)
		var rendered string
		switch entry.kind {
		case "user":
			rendered = m.theme.UserInput.Render("❯ " + text)
		case "agent":
			rendered = m.theme.AgentReply.Render(text)
		case "tool":
			rendered = m.theme.ToolRunning.Render("⟳ " + text)
		case "error":
			rendered = m.theme.Error.Render("✗ " + text)
		case "memory":
			rendered = m.theme.StatusKey.Render("  " + text)
		}
		if selected {
			rendered = m.theme.Selection.Render(rendered)
		}
		sb.WriteString(rendered)
		sb.WriteString("\n")
	}

	// Thinking block (collapsible, above log section).
	if len(m.thinking.Lines()) > 0 || m.thinking.Active() {
		if m.thinkingExpanded {
			sb.WriteString(m.renderThinkingBlock())
		} else {
			sb.WriteString(m.renderThinkingPreview())
		}
	}

	// Log section (collapsible, at bottom of output).
	if len(m.logEntries) > 0 && m.logExpanded {
		sb.WriteString(m.renderLogSection())
	}

	m.viewport.SetContent(sb.String())
	m.viewport.NotifyContentAdded()
}

// ensureLayout recalculates viewport height from cached dimensions.
func (m *Model) ensureLayout() {
	if m.height <= 0 {
		return
	}
	inputHeight := m.inputLineHeight()
	statusHeight := 3
	if m.tabBar.Visible() {
		statusHeight++
	}
	vpHeight := m.height - inputHeight - statusHeight
	if vpHeight < 1 {
		vpHeight = 1
	}
	m.viewport.SetHeight(vpHeight)
}

// inputLineHeight returns the number of visual lines the input prompt occupies.
func (m Model) inputLineHeight() int {
	inputText := StripPartialANSI(m.textarea.Value())
	avail := m.width - 2
	if avail < 1 {
		avail = 1
	}
	wrapped := wrapText(inputText, avail)
	lines := strings.Count(wrapped, "\n") + 1
	if lines < 1 {
		lines = 1
	}
	return lines
}

// cursorVisualPos returns the visual line and column of the cursor
// in the word-wrapped output for the given wrapped lines and original text.
func (m Model) cursorVisualPos(lines []string, text string, width int) (int, int) {
	cursorLine := m.textarea.Line()
	li := m.textarea.LineInfo()
	colRunes := li.ColumnOffset + li.StartColumn

	textLines := strings.Split(text, "\n")
	bytePos := 0
	for i := 0; i < cursorLine && i < len(textLines); i++ {
		bytePos += len(textLines[i]) + 1
	}
	if cursorLine < len(textLines) {
		runes := []rune(textLines[cursorLine])
		if colRunes > len(runes) {
			colRunes = len(runes)
		}
		bytePos += len(string(runes[:colRunes]))
	}
	if bytePos > len(text) {
		bytePos = len(text)
	}

	origPos := 0
	for li, line := range lines {
		lineLen := len(line)
		if origPos+lineLen > bytePos {
			return li, bytePos - origPos
		}
		if origPos+lineLen == bytePos {
			return li, lineLen
		}
		origPos += lineLen
		if origPos < len(text) {
			if text[origPos] == '\n' {
				origPos++
			} else if text[origPos] == ' ' && origPos+1 < len(text) && text[origPos+1] != '\n' {
				origPos++
			}
		}
	}
	return len(lines) - 1, len([]rune(lines[len(lines)-1]))
}

// View renders the entire UI.
func (m Model) View() string {
	m.ensureLayout()

	// Help overlay takes full screen when visible.
	if m.showHelp != nil && m.showHelp.Visible() {
		return m.renderHelpOverlay()
	}

	var sections []string

	// Output pane (top, scrollable).
	sections = append(sections, m.viewport.View())

	// New content indicator when follow mode paused.
	if !m.viewport.FollowMode() && m.viewport.NewContent() {
		sections = append(sections,
			m.theme.Spinner.Render("↓ new content"),
		)
	}

	// Command palette overlay.
	if m.commandPalette.Visible() {
		sections = append(sections, m.renderPaletteWithFilter())
	}

	// Which-key popup overlay.
	if m.whichKey.Visible() {
		sections = append(sections, m.whichKey.View())
	}

	// Completion popup.
	if m.showCompletion && len(m.complMatches) > 0 {
		popup := m.renderCompletionPopup()
		sections = append(sections, popup)
	}

	// Permission prompt.
	if m.permission != nil {
		perm := m.renderPermissionPrompt(m.width)
		sections = append(sections, perm)
	}

	// Halt-all confirmation prompt.
	if m.haltPrompt {
		sections = append(sections, m.renderHaltPrompt(m.width))
	}

	// Upper separator — show spinner when running.
	if m.running {
		elapsed := time.Since(m.startTime).Round(time.Second)
		spinnerText := fmt.Sprintf(
			"%s %s (%s)",
			m.spinner.View(),
			m.spinnerText,
			elapsed,
		)
		sections = append(sections,
			m.theme.Separator.Render(spinnerText),
		)
	} else {
		sections = append(sections,
			m.renderSeparator(""),
		)
	}

	// Input line — plain text, no lipgloss styling on typed chars.
	promptChar := m.theme.PromptNormal.Render("❯")
	if m.textarea.Focused() {
		promptChar = m.theme.PromptActive.Render("❯")
	}
	inputText := StripPartialANSI(m.textarea.Value())
	avail := m.width - 2
	if avail < 1 {
		avail = 1
	}
	wrapped := wrapText(inputText, avail)
	m.textarea.SetHeight(m.inputLineHeight())

	lines := strings.Split(wrapped, "\n")

	var promptSection strings.Builder
	if m.textarea.Focused() {
		visLine, visCol := m.cursorVisualPos(lines, inputText, avail)
		for i, line := range lines {
			if i == 0 {
				promptSection.WriteString(promptChar + " ")
			} else {
				promptSection.WriteString("  ")
			}

			if i == visLine {
				r := []rune(line)
				if visCol > len(r) {
					visCol = len(r)
				}
				promptSection.WriteString(string(r[:visCol]))
				promptSection.WriteString("█")
				promptSection.WriteString(string(r[visCol:]))
			} else {
				promptSection.WriteString(line)
			}
			promptSection.WriteString("\n")
		}
	} else {
		for i, line := range lines {
			if i == 0 {
				promptSection.WriteString(promptChar + " " + line)
			} else {
				promptSection.WriteString("  " + line)
			}
			promptSection.WriteString("\n")
		}
	}
	sections = append(sections, promptSection.String())

	// Reverse-i-search overlay.
	if m.showReverseSearch {
		sections = append(sections, m.renderReverseSearch())
	}

	// Lower separator.
	sections = append(sections, m.renderSeparator(""))

	// Tab bar (only when 2+ sessions).
	if m.tabBar.Visible() {
		sections = append(sections, m.renderTabBar())
	}

	// Status line (indented 2 spaces).
	sections = append(sections, "  "+m.renderStatus())

	result := strings.Join(sections, "\n")

	if m.height > 0 {
		lines := strings.Count(result, "\n") + 1
		if pad := m.height - lines; pad > 0 {
			result += strings.Repeat("\n", pad)
		}
	}

	return result
}

// renderCompletionPopup renders the slash-command completion list.
func (m Model) renderCompletionPopup() string {
	var b strings.Builder
	for i, cmd := range m.complMatches {
		prefix := "  "
		if i == m.complIdx {
			prefix = "▸ "
		}
		b.WriteString(fmt.Sprintf("%s%s  %s\n",
			prefix,
			m.theme.StatusValue.Render(cmd.Name),
			m.theme.StatusKey.Render(cmd.Description),
		))
	}
	return b.String()
}

// renderSeparator returns a horizontal rule, optionally with a label.
func (m Model) renderSeparator(label string) string {
	width := m.width
	if width <= 0 {
		width = 60
	}
	if label == "" {
		return m.theme.Separator.Render(
			strings.Repeat("─", width),
		)
	}
	prefix := strings.Repeat("─", 3)
	suffixLen := width - len(prefix) - len(label) - 2
	if suffixLen < 0 {
		suffixLen = 0
	}
	suffix := strings.Repeat("─", suffixLen)
	return m.theme.Separator.Render(
		prefix + " " + label + " " + suffix,
	)
}

// outputTexts extracts the text fields from output entries.
func outputTexts(entries []outputEntry) []string {
	texts := make([]string, len(entries))
	for i, e := range entries {
		texts[i] = e.text
	}
	return texts
}

// updateCtxStats refreshes ctxStats from the current session and compressor.
func (m *Model) updateCtxStats() {
	if m.session == nil || m.compressor == nil {
		return
	}
	mode := m.compressor.Config()
	cw := m.modelInfo.ContextWindow
	if cw == 0 {
		cw = mode.FallbackContextWindow
	}
	messages := m.session.Messages()
	estimated := m.compressor.EstimateMessages(messages)
	fallback := m.modelInfo.ContextWindow == 0 && cw == mode.FallbackContextWindow

	offloaded := m.compressor.OffloadCount
	storageBytes := int64(offloaded * 512)

	m.ctxStats = &CtxStats{
		EstimatedTokens:   estimated,
		ContextWindow:     cw,
		Compressions:      m.compressor.CompressionCount,
		Mode:              mode.Mode,
		Fallback:          fallback,
		OffloadedMessages: offloaded,
		StorageUsedBytes:  storageBytes,
	}
}

// renderReverseSearch renders the ctrl+r reverse-i-search overlay.
func (m Model) renderReverseSearch() string {
	var b strings.Builder
	b.WriteString(m.theme.PopupTitle.Render(" Ctrl-R Search"))
	b.WriteString(m.theme.PopupItem.Render(" query: " + m.reverseSearch.Query()))
	b.WriteString("\n")
	matches := m.reverseSearch.Matches()
	if len(matches) > 0 {
		b.WriteString(strings.Repeat("─", 40) + "\n")
		start := 0
		if len(matches) > 5 {
			start = m.reverseSearch.SelectedIndex()
		}
		end := start + 5
		if end > len(matches) {
			end = len(matches)
		}
		for i := start; i < end; i++ {
			prefix := "  "
			if i == m.reverseSearch.SelectedIndex() {
				prefix = "▸ "
			}
			b.WriteString(fmt.Sprintf("%s%s\n", prefix, matches[i]))
		}
		if len(matches) > 5 {
			b.WriteString(fmt.Sprintf("  (%d matches)\n", len(matches)))
		}
	}
	return b.String()
}

// renderTabBar renders the session tab bar.
func (m Model) renderTabBar() string {
	tabs := m.tabBar.Tabs()
	var b strings.Builder
	for i, t := range tabs {
		if i == m.tabBar.ActiveIndex() {
			b.WriteString(m.theme.TabActive.Render(
				" " + t.Label + " ",
			))
		} else {
			style := m.theme.TabInactive
			if t.Spinning {
				style = m.theme.TabSpin
			}
			b.WriteString(style.Render(" " + t.Label + " "))
		}
	}
	return b.String()
}

// resolveReferences finds @-refs in text, resolves them, and returns the
// cleaned text plus any resolved content to inject as context.
func (m *Model) resolveReferences(text string) (string, string) {
	var resolved []string
	remaining := text
	for {
		idx := strings.LastIndex(remaining, "@")
		if idx < 0 {
			break
		}
		// Only match @ at word boundary.
		if idx > 0 && remaining[idx-1] != ' ' {
			remaining = remaining[:idx]
			continue
		}
		query := remaining[idx+1:]
		if query == "" {
			break
		}
		// Find the end of the @ref token.
		end := idx + 1
		for end < len(remaining) && remaining[end] != ' ' {
			end++
		}
		alias := remaining[idx+1 : end]
		content, kind, err := m.referenceEngine.Resolve(alias)
		if err == nil && content != "" {
			resolved = append(resolved,
				fmt.Sprintf("[%s @%s]:\n%s", kind, alias, content),
			)
			remaining = remaining[:idx]
		} else {
			break
		}
	}
	if len(resolved) == 0 {
		return text, ""
	}
	cleaned := strings.TrimSpace(remaining)
	return cleaned, strings.Join(resolved, "\n\n")
}

// renderPaletteWithFilter renders the command palette with inline filter.
func (m Model) renderPaletteWithFilter() string {
	items := m.commandPalette.Filtered()
	var b strings.Builder
	b.WriteString(m.theme.PopupTitle.Render(" Command Palette") + "\n")
	b.WriteString(m.theme.FilterBox.Render(
		" filter: "+m.commandPalette.Filter()+"_",
	) + "\n")
	b.WriteString(strings.Repeat("─", 40) + "\n")
	for i, item := range items {
		prefix := "  "
		if i == m.commandPalette.SelectedIndex() {
			prefix = "▸ "
		}
		b.WriteString(fmt.Sprintf("%s%s  %s\n",
			prefix,
			m.theme.PopupItem.Render(item.Name),
			m.theme.PopupDesc.Render(item.Description),
		))
	}
	if len(items) == 0 {
		b.WriteString(m.theme.PopupDesc.Render("  (no matches)") + "\n")
	}
	return b.String()
}

// handlePaletteKey routes key events when the command palette is visible.
func (m Model) handlePaletteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.commandPalette.Dismiss()
		return m, nil
	case tea.KeyCtrlP:
		// Ctrl+P again dismisses the palette.
		m.commandPalette.Dismiss()
		return m, nil
	case tea.KeyEnter:
		m.commandPalette.Execute()
		return m, nil
	case tea.KeyUp:
		m.commandPalette.MoveUp()
		return m, nil
	case tea.KeyDown:
		m.commandPalette.MoveDown()
		return m, nil
	case tea.KeyBackspace, tea.KeyDelete:
		filter := m.commandPalette.Filter()
		if len(filter) > 0 {
			filter = filter[:len(filter)-1]
		}
		m.commandPalette.SetFilter(filter)
		return m, nil
	}
	// Any printable rune → append to filter.
	if msg.Type == tea.KeyRunes {
		filter := m.commandPalette.Filter() + string(msg.Runes)
		m.commandPalette.SetFilter(filter)
		return m, nil
	}
	return m, nil
}

// handleReverseSearchKey routes key events when reverse-i-search is visible.
func (m Model) handleReverseSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.reverseSearch.Dismiss()
		m.showReverseSearch = false
		return m, nil
	case tea.KeyEnter:
		sel := m.reverseSearch.Selected()
		m.reverseSearch.Dismiss()
		m.showReverseSearch = false
		if sel != "" {
			m.textarea.SetValue(sel)
		}
		return m, nil
	case tea.KeyCtrlR:
		// Ctrl+R again cycles forward through matches.
		m.reverseSearch.CycleForward()
		return m, nil
	case tea.KeyCtrlS:
		// Ctrl+S cycles backward through matches.
		m.reverseSearch.CycleReverse()
		return m, nil
	case tea.KeyBackspace, tea.KeyDelete:
		q := m.reverseSearch.Query()
		if len(q) > 0 {
			q = q[:len(q)-1]
		}
		m.reverseSearch.SetQuery(q)
		return m, nil
	}
	// Any printable rune → append to query.
	if msg.Type == tea.KeyRunes {
		for _, r := range msg.Runes {
			m.reverseSearch.AddRune(r)
		}
		return m, nil
	}
	return m, nil
}
