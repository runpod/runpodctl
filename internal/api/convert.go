package api

import "github.com/runpod/go-sdk/v2/lib/types"

// Converters from the generated SDK's types to the structs the cli prints. The
// cli keeps its own structs so that its JSON output does not change shape when
// the API or the SDK does.

func networkVolumeFromSDK(v types.NetworkVolume) NetworkVolume {
	return NetworkVolume{ID: v.Id, Name: v.Name, Size: v.Size, DataCenterID: v.DataCenter}
}
