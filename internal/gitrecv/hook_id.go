package gitrecv

func EventID(deploymentID string, suffix string) string {
	return "evt_" + deploymentID + "_" + suffix
}
