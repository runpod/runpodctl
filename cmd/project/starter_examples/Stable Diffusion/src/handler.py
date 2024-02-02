''' A starter handler file using RunPod and diffusers for image generation. '''

import io
import base64
from typing import Dict

import runpod
from diffusers import AutoPipelineForText2Image
import torch

# Initialize the pipeline
pipe = AutoPipelineForText2Image.from_pretrained(
            "stabilityai/sdxl-turbo", # model name
            torch_dtype=torch.float16, variant="fp16"
        ).to("cuda")


def handler(job: Dict[str, any]) -> str:
    """
    Handler function for processing a job.

    Args:
        job (dict): A dictionary containing the job input.

    Returns:
        str: A base64 encoded string of the generated image.
    """

    job_input = job['input']
    prompt = job_input['prompt']

    image = pipe(prompt=prompt, num_inference_steps=1, guidance_scale=0.0).images[0]

    with io.BytesIO() as buffer:
        image.save(buffer, format="PNG")
        image_bytes = buffer.getvalue()
        base64_image = base64.b64encode(image_bytes).decode('utf-8')

    return f"data:image/png;base64,{base64_image}"


runpod.serverless.start({"handler": handler})
