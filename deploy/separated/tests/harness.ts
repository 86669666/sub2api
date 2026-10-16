import * as batch from '../../../frontend/src/api/batchImage'
import { gatewayFetch } from '../../../frontend/src/api/gateway'
import { getDeploymentContract } from '../../../frontend/src/api/url'
import { subscribeQPS } from '../../../frontend/src/api/admin/ops'
Object.assign(window, {d0: {batch, gatewayFetch, getDeploymentContract, subscribeQPS}})
