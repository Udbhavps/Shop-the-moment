#!/usr/bin/env bash
# Deploy Shop the Moment to Cloud Run.  Usage: PROJECT_ID=my-proj ./deploy/deploy.sh
set -euo pipefail
: "${PROJECT_ID:?set PROJECT_ID}"
REGION="${REGION:-asia-south1}"          # Mumbai; pick any Cloud Run region
SERVICE="${SERVICE:-shop-the-moment}"
SA="stm-runner"

gcloud config set project "$PROJECT_ID"
gcloud services enable run.googleapis.com aiplatform.googleapis.com cloudbuild.googleapis.com artifactregistry.googleapis.com

# Newer projects do not give the default build account the roles that deploying
# from source needs. Grant them (safe to repeat).
PROJECT_NUMBER=$(gcloud projects describe "$PROJECT_ID" --format 'value(projectNumber)')
BUILD_SA="$PROJECT_NUMBER-compute@developer.gserviceaccount.com"
for role in roles/cloudbuild.builds.builder roles/storage.objectViewer roles/artifactregistry.writer roles/logging.logWriter; do
  gcloud projects add-iam-policy-binding "$PROJECT_ID" --member "serviceAccount:$BUILD_SA" --role "$role" --condition=None >/dev/null
done

gcloud iam service-accounts describe "$SA@$PROJECT_ID.iam.gserviceaccount.com" >/dev/null 2>&1 || \
  gcloud iam service-accounts create "$SA" --display-name "Shop the Moment runtime"
gcloud projects add-iam-policy-binding "$PROJECT_ID" \
  --member "serviceAccount:$SA@$PROJECT_ID.iam.gserviceaccount.com" --role roles/aiplatform.user >/dev/null

# max-instances=1 is required: sessions, carts and events live in memory.
gcloud run deploy "$SERVICE" --source . --region "$REGION" \
  --service-account "$SA@$PROJECT_ID.iam.gserviceaccount.com" \
  --allow-unauthenticated --max-instances=1 --min-instances=1 --cpu=1 --memory=512Mi \
  --set-env-vars "GOOGLE_CLOUD_PROJECT=$PROJECT_ID,GEMINI_LOCATION=${GEMINI_LOCATION:-global},GEMINI_MODEL=${GEMINI_MODEL:-gemini-2.5-flash}"

URL=$(gcloud run services describe "$SERVICE" --region "$REGION" --format 'value(status.url)')
# QR codes need to point at the public URL.
gcloud run services update "$SERVICE" --region "$REGION" --update-env-vars "PUBLIC_BASE_URL=$URL"
echo "Deployed: $URL"
echo "Check live AI: curl -s $URL/api/config   (expect \"live_ai\": true)"
