''' A starter handler file using RunPod and transformers for audio generation. '''

import io
import base64
from typing import Dict

import scipy.io.wavfile
from transformers import pipeline

import runpod


# Initialize the pipeline
synthesizer = pipeline("text-to-audio", "<<MODEL_NAME>>", device=0)


def handler(job):
    """
    Processes a text prompt to generate music, returning the result as a base64-encoded WAV audio.

    Args:
        job (dict): Contains 'input' with a 'prompt' key for the music generation text prompt.

    Returns:
        str: The generated audio as a base64-encoded string.
    """
    prompt = job['input']['prompt']
    print(f"Received prompt: {prompt}")

    result = synthesizer(prompt, forward_params={"do_sample": True, "max_new_tokens":300})

    audio_data = result['audio']
    sample_rate = result['sampling_rate']

    # Prepare an in-memory bytes buffer to save the audio
    audio_bytes = io.BytesIO()
    scipy.io.wavfile.write(audio_bytes, sample_rate, audio_data)
    audio_bytes.seek(0)

    # Encode the WAV file to a base64 string
    base64_audio = base64.b64encode(audio_bytes.read()).decode('utf-8')

    # Return the base64 encoded audio with the appropriate data URI scheme
    return f"data:audio/wav;base64,{base64_audio}"


runpod.serverless.start({"handler": handler})
