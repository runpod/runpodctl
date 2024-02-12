''' A template for a handler file. '''

import runpod

def handler(job):
    '''
    This is the handler function for the job.
    '''
    job_input = job['input']
    name = job_input.get('name', 'World')
    return f"Hello, {name}!"

runpod.serverless.start({"handler": handler})
