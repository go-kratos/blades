package blades

// MaxIterations sets the maximum number of iterations for the model.
func MaxIterations(n int) ModelOption {
	return func(o *ModelOptions) {
		o.MaxIterations = n
	}
}

// MaxOutputTokens sets the maximum number of tokens to generate in the response.
func MaxOutputTokens(n int64) ModelOption {
	return func(o *ModelOptions) {
		o.MaxOutputTokens = n
	}
}

// TopP sets the nucleus sampling parameter.
func TopP(p float64) ModelOption {
	return func(o *ModelOptions) {
		o.TopP = p
	}
}

// Temperature sets the sampling temperature to use, between 0.0 and 1.0.
func Temperature(t float64) ModelOption {
	return func(o *ModelOptions) {
		o.Temperature = t
	}
}

// ReasoningEffort sets the level of reasoning effort for the model.
func ReasoningEffort(effort string) ModelOption {
	return func(o *ModelOptions) {
		o.ReasoningEffort = effort
	}
}

// ImageBackground sets the image background preference.
func ImageBackground(background string) ModelOption {
	return func(o *ModelOptions) {
		o.Image.Background = background
	}
}

// ImageSize sets the requested output dimensions for generated images.
func ImageSize(size string) ModelOption {
	return func(o *ModelOptions) {
		o.Image.Size = size
	}
}

// ImageQuality sets the quality preset for generated images.
func ImageQuality(quality string) ModelOption {
	return func(o *ModelOptions) {
		o.Image.Quality = quality
	}
}

// ImageResponseFormat sets the response format (e.g. b64_json or url).
func ImageResponseFormat(format string) ModelOption {
	return func(o *ModelOptions) {
		o.Image.ResponseFormat = format
	}
}

// ImageOutputFormat sets the output encoding format (e.g. png, jpeg, webp).
func ImageOutputFormat(format string) ModelOption {
	return func(o *ModelOptions) {
		o.Image.OutputFormat = format
	}
}

// ImageModeration sets the moderation level for generated images.
func ImageModeration(level string) ModelOption {
	return func(o *ModelOptions) {
		o.Image.Moderation = level
	}
}

// ImageStyle sets the style hint for generated images.
func ImageStyle(style string) ModelOption {
	return func(o *ModelOptions) {
		o.Image.Style = style
	}
}

// ImageUser tags the generated images with an end-user identifier.
func ImageUser(user string) ModelOption {
	return func(o *ModelOptions) {
		o.Image.User = user
	}
}

// ImageCount sets how many images to request.
func ImageCount(n int) ModelOption {
	return func(o *ModelOptions) {
		o.Image.Count = n
	}
}

// ImagePartialImages sets how many partial images to emit (for streaming APIs).
func ImagePartialImages(n int) ModelOption {
	return func(o *ModelOptions) {
		o.Image.PartialImages = n
	}
}

// ImageOutputCompression sets the output compression percentage for JPEG/WEBP.
func ImageOutputCompression(percent int) ModelOption {
	return func(o *ModelOptions) {
		o.Image.OutputCompression = percent
	}
}

// AudioVoice selects the synthetic voice for generated speech.
func AudioVoice(voice string) ModelOption {
	return func(o *ModelOptions) {
		o.Audio.Voice = voice
	}
}

// AudioResponseFormat sets the audio container/codec returned by the provider.
func AudioResponseFormat(format string) ModelOption {
	return func(o *ModelOptions) {
		o.Audio.ResponseFormat = format
	}
}

// AudioStreamFormat selects the streaming protocol, when supported.
func AudioStreamFormat(format string) ModelOption {
	return func(o *ModelOptions) {
		o.Audio.StreamFormat = format
	}
}

// AudioInstructions provide additional guidance on the delivery of speech.
func AudioInstructions(instructions string) ModelOption {
	return func(o *ModelOptions) {
		o.Audio.Instructions = instructions
	}
}

// AudioSpeed sets the playback speed multiplier (0.25 - 4.0).
func AudioSpeed(speed float64) ModelOption {
	return func(o *ModelOptions) {
		o.Audio.Speed = speed
	}
}
