''' A starter handler file using RunPod and a large language model for text generation. '''

import io
import base64
from typing import Dict

import runpod
from transformers import T5Tokenizer, T5ForConditionalGeneration

# Initialize the tokenizer and model
tokenizer = T5Tokenizer.from_pretrained("<<MODEL_NAME>>")
model = T5ForConditionalGeneration.from_pretrained("<<MODEL_NAME>>", device_map="auto").to("cuda")


def handler(job: Dict[str, any]) -> str:
    """
    Handler function for processing a job.

    Args:
        job (dict): A dictionary containing the job input.

    Returns:
        str: The generated text response.
    """

    job_input = job['input']
    input_text = job_input['text']

    input_ids = tokenizer(input_text, return_tensors="pt").input_ids.to("cuda")
    outputs = model.generate(input_ids)
    response = tokenizer.decode(outputs[0])

    return response


runpod.serverless.start({"handler": handler})
