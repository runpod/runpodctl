''' A template for a Llama2 handler file. '''
# pylint: skip-file

import runpod
import inspect
from transformers import HfApi

SELECTED_MODEL = "<<MODEL_NAME>>"

def get_model_framework(model_name):
    api = HfApi()
    model_files = api.model_info(model_name).files

    # Check the files associated with the model
    if "pytorch_model.bin" in model_files:
        return "PyTorch"
    elif "tf_model.h5" in model_files:
        return "TensorFlow"
    else:
        return "Unknown"

def prepare_inputs(text, **kwargs):
    # Filter kwargs based on what the tokenizer accepts
    filtered_args = {k: v for k, v in kwargs.items() if k in valid_args}

    inputs = tokenizer(text, return_tensors="pt", **filtered_args)
    return inputs


def handle_request(text, **input_args):
    inputs = prepare_inputs(text, **input_args)
    with torch.no_grad():
        outputs = model(**inputs)
    return process_outputs(outputs)

runpod.serverless.start({"handler": handle_request})
